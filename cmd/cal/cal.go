package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	api "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
	htrans "google.golang.org/api/transport/http"
)

var (
	credsFile  = flag.String("creds", "", "filename for creds")
	id         = flag.String("id", "", "ID of calendar (typically, user email address)")
	eventFile  = flag.String("events", "", "filename of events")
	startIndex = flag.Int("start", 1, "1-based event to start inserting at")
	endIndex   = flag.Int("end", -1, "1-based event to end inserting at, inclusive")
	doit       = flag.Bool("doit", false, "nothing happens unless this is provided")
)

func main() {
	flag.Parse()
	ctx := context.Background()
	if *credsFile == "" {
		log.Fatal("need -creds")
	}
	if *id == "" {
		log.Fatal("need -id")
	}
	if *eventFile == "" {
		log.Fatal("need -events")
	}

	hc, _, err := htrans.NewClient(ctx, option.WithCredentialsFile(*credsFile))
	if err != nil {
		log.Fatal(err)
	}
	client, err := api.New(hc)
	if err != nil {
		log.Fatal(err)
	}
	evs, err := readEventFile(*eventFile)
	if err != nil {
		log.Fatal(err)
	}
	start := *startIndex - 1
	end := *endIndex - 1
	if end < 0 || end >= len(evs) {
		end = len(evs) - 1
	}
	fmt.Printf("start=%d, end=%d\n", start, end)
	if !*doit {
		fmt.Println("provide -doit to insert")
		return
	}
	n := 0
	for i := start; i <= end; i++ {
		ev := evs[i]
		err := insertEvent(ctx, client, *id, ev)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("inserted %s - %s\t%q\t%s\n", ev.Start.DateTime, ev.End.DateTime, ev.Summary, ev.Description)
		n++
	}
	fmt.Printf("inserted %d events.\n", n)
}

// File format: blank-line-separated events, each of which is:
//		Friday January 19
//		7:00pm – 9:00pm
//      summary
//		optional description line 1
//		optional description line 2
//		...
func readEventFile(filename string) ([]*api.Event, error) {
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var evs []*api.Event
	for _, sev := range strings.Split(string(bytes), "\n\n") {
		e, err := parseEvent(sev)
		if err != nil {
			return nil, err
		}
		evs = append(evs, e)
	}
	return evs, nil
}

func parseEvent(e string) (*api.Event, error) {
	lines := strings.Split(e, "\n")
	// Trim whitespace, replace en-dash with hyphen.
	for i := range lines {
		lines[i] = strings.Replace(strings.TrimSpace(lines[i]),
			"–", "-", -1)
	}
	if len(lines) < 3 {
		return nil, fmt.Errorf("too few lines: %q", e)
	}
	date := lines[0]
	times := strings.Split(lines[1], "-")
	if len(times) != 2 {
		return nil, fmt.Errorf("bad time line: %q\n", lines[1])
	}
	summary := lines[2]
	desc := strings.Join(lines[3:], "\n")

	start, err := parseTime(date + " " + times[0])
	if err != nil {
		return nil, err
	}
	end, err := parseTime(date + " " + times[1])
	if err != nil {
		return nil, err
	}
	return &api.Event{
		Start:       &api.EventDateTime{DateTime: start.Format(time.RFC3339)},
		End:         &api.EventDateTime{DateTime: end.Format(time.RFC3339)},
		Summary:     summary,
		Description: desc,
	}, nil
}

// e.g. "2018 January 17 5:30pm"
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// First try without minutes.
	t, err := time.ParseInLocation("2006 January 2 3pm", s, time.Local)
	if err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006 January 2 3:04pm", s, time.Local)
}

func insertEvent(ctx context.Context, c *api.Service, calID string, ev *api.Event) error {
	_, err := c.Events.Insert(calID, ev).Context(ctx).Do()
	return err
}

func listEvents(ctx context.Context, c *api.Service, calID string) {
	call := c.Events.List(calID).Context(ctx)
	call.SingleEvents(true)
	call.OrderBy("startTime")
	tm := time.Now().Format(time.RFC3339)
	fmt.Println(tm)
	call.TimeMin(tm)
	events, err := call.Do()
	if err != nil {
		log.Fatal(err)
	}
	for i, e := range events.Items {
		fmt.Printf("%d: Start:%s End:%s  Summary:%s\n",
			i, eventTime(e.Start), eventTime(e.End), e.Summary)
	}
}

func eventTime(dt *api.EventDateTime) string {
	if dt.Date != "" {
		return dt.Date
	}
	return dt.DateTime
}

// List all calendars that the authenticated user has access to.
func listCalendars(c *api.Service) {
	clist, err := c.CalendarList.List().Do()
	if err != nil {
		log.Fatal(err)
	}
	for i, e := range clist.Items {
		fmt.Printf("%d: ID:%q Primary:%t Summary:%q\n",
			i, e.Id, e.Primary, e.Summary)
	}
}

var ocfg = &oauth2.Config{
	ClientID:     "CLIENT ID FOR MY PROJECT",
	ClientSecret: "CLIENT SECRET FOR MY PROJECT",
	Endpoint:     google.Endpoint,
	RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
	Scopes:       []string{api.CalendarScope},
}

// Call this once to get creds. The resulting JSON should be stored
// in a protected file whose name should be passed to -creds.
func getUserConsentManual(cfg *oauth2.Config) {
	url := ocfg.AuthCodeURL("xyzzy", oauth2.AccessTypeOffline)
	fmt.Println("have the user visit this url:")
	fmt.Println(url)
	fmt.Println("Take the resulting auth code and paste it here, then hit return:")
	var code string
	fmt.Scanf("%s", &code)
	fmt.Printf("code = %q\n", code)

	tok, err := ocfg.Exchange(context.Background(), code)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("save this JSON file:")
	fmt.Printf(`
{
    "type": "authorized_user",
    "client_id": %q,
    "client_secret": %q,
    "refresh_token": %q
}\n`, ocfg.ClientID, ocfg.ClientSecret, tok.RefreshToken)
}
