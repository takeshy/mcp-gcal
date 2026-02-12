package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// CalendarService wraps the Google Calendar API.
type CalendarService struct {
	svc *calendar.Service
}

// NewCalendarService creates a Calendar API client from a token source.
func NewCalendarService(ctx context.Context, ts oauth2.TokenSource) (*CalendarService, error) {
	svc, err := calendar.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return &CalendarService{svc: svc}, nil
}

// JSON output types

type eventJSON struct {
	ID          string         `json:"id"`
	Summary     string         `json:"summary"`
	Description string         `json:"description,omitempty"`
	Location    string         `json:"location,omitempty"`
	Start       *dateTimeJSON  `json:"start,omitempty"`
	End         *dateTimeJSON  `json:"end,omitempty"`
	Status      string         `json:"status,omitempty"`
	HTMLLink    string         `json:"htmlLink,omitempty"`
	Attendees   []attendeeJSON `json:"attendees,omitempty"`
	Organizer   *organizerJSON `json:"organizer,omitempty"`
	Created     string         `json:"created,omitempty"`
	Updated     string         `json:"updated,omitempty"`
}

type dateTimeJSON struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type attendeeJSON struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
}

type organizerJSON struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName,omitempty"`
	Self        bool   `json:"self,omitempty"`
}

type calendarJSON struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Primary     bool   `json:"primary,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

func convertEvent(e *calendar.Event) eventJSON {
	ev := eventJSON{
		ID:          e.Id,
		Summary:     e.Summary,
		Description: e.Description,
		Location:    e.Location,
		Status:      e.Status,
		HTMLLink:    e.HtmlLink,
		Created:     e.Created,
		Updated:     e.Updated,
	}
	if e.Start != nil {
		ev.Start = &dateTimeJSON{
			DateTime: e.Start.DateTime,
			Date:     e.Start.Date,
			TimeZone: e.Start.TimeZone,
		}
	}
	if e.End != nil {
		ev.End = &dateTimeJSON{
			DateTime: e.End.DateTime,
			Date:     e.End.Date,
			TimeZone: e.End.TimeZone,
		}
	}
	for _, a := range e.Attendees {
		ev.Attendees = append(ev.Attendees, attendeeJSON{
			Email:          a.Email,
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
			Self:           a.Self,
		})
	}
	if e.Organizer != nil {
		ev.Organizer = &organizerJSON{
			Email:       e.Organizer.Email,
			DisplayName: e.Organizer.DisplayName,
			Self:        e.Organizer.Self,
		}
	}
	return ev
}

// ListCalendars returns all calendars accessible to the user.
func (cs *CalendarService) ListCalendars() ([]calendarJSON, error) {
	list, err := cs.svc.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	result := make([]calendarJSON, 0, len(list.Items))
	for _, c := range list.Items {
		result = append(result, calendarJSON{
			ID:          c.Id,
			Summary:     c.Summary,
			Description: c.Description,
			Primary:     c.Primary,
			TimeZone:    c.TimeZone,
		})
	}
	return result, nil
}

// ListEvents lists events in a calendar within a time range.
func (cs *CalendarService) ListEvents(calendarID, timeMin, timeMax string, maxResults int64, singleEvents bool, orderBy string) ([]eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	now := time.Now()
	if timeMin == "" {
		timeMin = now.Format(time.RFC3339)
	}
	if timeMax == "" {
		timeMax = now.AddDate(0, 0, 7).Format(time.RFC3339)
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	call := cs.svc.Events.List(calendarID).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(maxResults).
		SingleEvents(singleEvents)

	if orderBy != "" {
		call = call.OrderBy(orderBy)
	}

	events, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	result := make([]eventJSON, 0, len(events.Items))
	for _, e := range events.Items {
		result = append(result, convertEvent(e))
	}
	return result, nil
}

// GetEvent retrieves a single event by ID.
func (cs *CalendarService) GetEvent(calendarID, eventID string) (*eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	e, err := cs.svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	ev := convertEvent(e)
	return &ev, nil
}

// SearchEvents searches events by text query.
func (cs *CalendarService) SearchEvents(calendarID, query, timeMin, timeMax string, maxResults int64) ([]eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}
	now := time.Now()
	if timeMin == "" {
		timeMin = now.Format(time.RFC3339)
	}
	if timeMax == "" {
		timeMax = now.AddDate(0, 0, 7).Format(time.RFC3339)
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	events, err := cs.svc.Events.List(calendarID).
		Q(query).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(maxResults).
		SingleEvents(true).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}

	result := make([]eventJSON, 0, len(events.Items))
	for _, e := range events.Items {
		result = append(result, convertEvent(e))
	}
	return result, nil
}

// CreateEvent creates a new calendar event.
func (cs *CalendarService) CreateEvent(calendarID, summary, description, location, start, end, timezone, attendees string) (*eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}

	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Location:    location,
	}

	startIsDate := isDateOnly(start)
	endIsDate := isDateOnly(end)

	// Google Calendar API requires start and end to both be date or both be dateTime.
	// If they're mixed, convert the date to dateTime (start of day in the given timezone).
	if startIsDate != endIsDate {
		tz := timezone
		if tz == "" {
			tz = "UTC"
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		if startIsDate {
			t, _ := time.ParseInLocation("2006-01-02", start, loc)
			start = t.Format(time.RFC3339)
			startIsDate = false
		}
		if endIsDate {
			t, _ := time.ParseInLocation("2006-01-02", end, loc)
			end = t.Format(time.RFC3339)
			endIsDate = false
		}
	}

	if startIsDate {
		event.Start = &calendar.EventDateTime{Date: start}
	} else {
		event.Start = &calendar.EventDateTime{DateTime: start}
	}
	if timezone != "" && event.Start != nil {
		event.Start.TimeZone = timezone
	}

	if endIsDate {
		event.End = &calendar.EventDateTime{Date: end}
	} else {
		event.End = &calendar.EventDateTime{DateTime: end}
	}
	if timezone != "" && event.End != nil {
		event.End.TimeZone = timezone
	}

	if attendees != "" {
		for _, email := range strings.Split(attendees, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				event.Attendees = append(event.Attendees, &calendar.EventAttendee{Email: email})
			}
		}
	}

	created, err := cs.svc.Events.Insert(calendarID, event).Do()
	if err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}
	ev := convertEvent(created)
	return &ev, nil
}

// UpdateEvent updates an existing calendar event with the provided fields.
func (cs *CalendarService) UpdateEvent(calendarID, eventID string, updates map[string]string) (*eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}

	existing, err := cs.svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return nil, fmt.Errorf("get event for update: %w", err)
	}

	if v, ok := updates["summary"]; ok {
		existing.Summary = v
	}
	if v, ok := updates["description"]; ok {
		existing.Description = v
	}
	if v, ok := updates["location"]; ok {
		existing.Location = v
	}
	if v, ok := updates["start"]; ok {
		if isDateOnly(v) {
			existing.Start = &calendar.EventDateTime{Date: v}
		} else {
			start := &calendar.EventDateTime{DateTime: v}
			if existing.Start != nil {
				start.TimeZone = existing.Start.TimeZone
			}
			existing.Start = start
		}
	}
	if v, ok := updates["end"]; ok {
		if isDateOnly(v) {
			existing.End = &calendar.EventDateTime{Date: v}
		} else {
			end := &calendar.EventDateTime{DateTime: v}
			if existing.End != nil {
				end.TimeZone = existing.End.TimeZone
			}
			existing.End = end
		}
	}

	// Ensure start and end are both date or both dateTime
	startIsDate := existing.Start != nil && existing.Start.Date != ""
	endIsDate := existing.End != nil && existing.End.Date != ""
	if existing.Start != nil && existing.End != nil && startIsDate != endIsDate {
		tz := ""
		if existing.Start != nil {
			tz = existing.Start.TimeZone
		}
		if tz == "" && existing.End != nil {
			tz = existing.End.TimeZone
		}
		loc := time.UTC
		if tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				loc = l
			}
		}
		if startIsDate {
			t, _ := time.ParseInLocation("2006-01-02", existing.Start.Date, loc)
			existing.Start = &calendar.EventDateTime{DateTime: t.Format(time.RFC3339), TimeZone: tz}
		}
		if endIsDate {
			t, _ := time.ParseInLocation("2006-01-02", existing.End.Date, loc)
			existing.End = &calendar.EventDateTime{DateTime: t.Format(time.RFC3339), TimeZone: tz}
		}
	}
	if v, ok := updates["attendees"]; ok {
		existing.Attendees = nil
		for _, email := range strings.Split(v, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				existing.Attendees = append(existing.Attendees, &calendar.EventAttendee{Email: email})
			}
		}
	}

	updated, err := cs.svc.Events.Update(calendarID, eventID, existing).Do()
	if err != nil {
		return nil, fmt.Errorf("update event: %w", err)
	}
	ev := convertEvent(updated)
	return &ev, nil
}

// DeleteEvent deletes a calendar event.
func (cs *CalendarService) DeleteEvent(calendarID, eventID string) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	return cs.svc.Events.Delete(calendarID, eventID).Do()
}

// RespondToEvent updates the authenticated user's response to an event invitation.
func (cs *CalendarService) RespondToEvent(calendarID, eventID, response string) (*eventJSON, error) {
	if calendarID == "" {
		calendarID = "primary"
	}

	switch response {
	case "accepted", "declined", "tentative":
	default:
		return nil, fmt.Errorf("invalid response: %s (must be accepted, declined, or tentative)", response)
	}

	event, err := cs.svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	found := false
	for _, a := range event.Attendees {
		if a.Self {
			a.ResponseStatus = response
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("you are not an attendee of this event")
	}

	updated, err := cs.svc.Events.Update(calendarID, eventID, event).SendUpdates("all").Do()
	if err != nil {
		return nil, fmt.Errorf("update response: %w", err)
	}
	ev := convertEvent(updated)
	return &ev, nil
}

// isDateOnly returns true if s looks like a date-only string (YYYY-MM-DD).
func isDateOnly(s string) bool {
	return len(s) == 10 && s[4] == '-' && s[7] == '-'
}
