package schedule

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// The Windows install is defined by a Task Scheduler XML document, not by a
// schtasks /TR string. The XML carries the calendar trigger whose StartBoundary
// is the NEXT occurrence of the chosen time (so a fresh task is never a missed
// start) and the resilience settings the single-PC persona needs: a run missed
// while the machine slept is caught up when it next wakes, and battery power no
// longer blocks or stops it. Every text node is written by encoding/xml, so a
// path holding "&", "<", a quote or a non-ASCII rune is escaped and the document
// round-trips through xml.Unmarshal. WakeToRun stays false: the task cannot
// power on an off machine (documented as a weak point). Real-host acceptance of
// the document by schtasks.exe is lab-pending (MA-79).

// taskNamespace is the Task Scheduler schema namespace for the 1.2 format.
const taskNamespace = "http://schemas.microsoft.com/windows/2004/02/mit/task"

// The marshalled document. Field order is the element order schtasks itself
// emits, so a real host reads a familiar shape. The namespace is written as a
// plain default-namespace attribute (xmlns=…); because the struct tags carry no
// namespace, xml.Unmarshal then matches every element by local name, so the
// document round-trips regardless of the namespace.
type taskXML struct {
	XMLName          xml.Name            `xml:"Task"`
	Version          string              `xml:"version,attr"`
	Xmlns            string              `xml:"xmlns,attr"`
	RegistrationInfo taskRegistrationXML `xml:"RegistrationInfo"`
	Triggers         taskTriggersXML     `xml:"Triggers"`
	Settings         taskSettingsXML     `xml:"Settings"`
	Actions          taskActionsXML      `xml:"Actions"`
}

type taskRegistrationXML struct {
	Description string `xml:"Description"`
}

type taskTriggersXML struct {
	Calendar taskCalendarTriggerXML `xml:"CalendarTrigger"`
}

type taskCalendarTriggerXML struct {
	StartBoundary string              `xml:"StartBoundary"`
	Enabled       bool                `xml:"Enabled"`
	Repetition    *taskRepetitionXML  `xml:"Repetition,omitempty"`
	ByDay         *taskScheduleDayXML `xml:"ScheduleByDay,omitempty"`
	ByWeek        *taskScheduleWkXML  `xml:"ScheduleByWeek,omitempty"`
}

type taskRepetitionXML struct {
	Interval          string `xml:"Interval"`
	StopAtDurationEnd bool   `xml:"StopAtDurationEnd"`
}

type taskScheduleDayXML struct {
	DaysInterval int `xml:"DaysInterval"`
}

type taskScheduleWkXML struct {
	WeeksInterval int             `xml:"WeeksInterval"`
	DaysOfWeek    taskDaysOfWeekX `xml:"DaysOfWeek"`
}

type taskDaysOfWeekX struct {
	Sunday *struct{} `xml:"Sunday"`
}

type taskSettingsXML struct {
	StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
	DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
	MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
	WakeToRun                  bool   `xml:"WakeToRun"`
}

type taskActionsXML struct {
	Exec taskExecXML `xml:"Exec"`
}

type taskExecXML struct {
	Command   string `xml:"Command"`
	Arguments string `xml:"Arguments,omitempty"`
}

// xmlDeclaration is the document prolog. It declares UTF-16 because the file is
// written UTF-16LE with a byte-order mark, which is what schtasks /XML expects.
const xmlDeclaration = `<?xml version="1.0" encoding="UTF-16"?>` + "\n"

// SchtasksXML returns the Task Scheduler definition for the spec, encoded
// UTF-16LE with a byte-order mark (what schtasks /Create /XML reads). now fixes
// the trigger's StartBoundary via nextOccurrence, so the value is deterministic
// for a given clock and the fresh task's first fire is always in the future.
func SchtasksXML(s Spec, now time.Time) ([]byte, error) {
	iv, err := ParseInterval(string(s.Interval))
	if err != nil {
		return nil, err
	}
	hour, min, err := parseHHMM(s.At)
	if err != nil {
		return nil, err
	}

	trig := taskCalendarTriggerXML{
		StartBoundary: nextOccurrence(now, iv, hour, min).Format("2006-01-02T15:04:05"),
		Enabled:       true,
	}
	switch iv {
	case Weekly:
		trig.ByWeek = &taskScheduleWkXML{WeeksInterval: 1, DaysOfWeek: taskDaysOfWeekX{Sunday: &struct{}{}}}
	case Hourly:
		// Task Scheduler has no "hourly" calendar schedule: run daily and repeat
		// once an hour within the day (indefinitely — no Duration).
		trig.ByDay = &taskScheduleDayXML{DaysInterval: 1}
		trig.Repetition = &taskRepetitionXML{Interval: "PT1H"}
	default: // Daily
		trig.ByDay = &taskScheduleDayXML{DaysInterval: 1}
	}

	act := taskExecXML{}
	if s.Wrapper {
		act.Command = s.WrapperPath
	} else {
		act.Command = s.Exe
		act.Arguments = taskArguments(s.Args)
	}

	doc := taskXML{
		Version:          "1.2",
		Xmlns:            taskNamespace,
		RegistrationInfo: taskRegistrationXML{Description: "mailarchive scheduled backup " + s.Name},
		Triggers:         taskTriggersXML{Calendar: trig},
		Settings: taskSettingsXML{
			StartWhenAvailable:         true,
			DisallowStartIfOnBatteries: false,
			StopIfGoingOnBatteries:     false,
			MultipleInstancesPolicy:    "IgnoreNew",
			WakeToRun:                  false,
		},
		Actions: taskActionsXML{Exec: act},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	utf8 := append([]byte(xmlDeclaration), body...)
	utf8 = append(utf8, '\n')
	return encodeUTF16LE(string(utf8)), nil
}

// taskArguments quotes each job argument for the Arguments element (used only
// when there is no wrapper): the scheduled program parses this string with the
// Windows C-runtime rules, so each token is winArg-quoted — the same quoting
// the /TR run string used — and joined by spaces.
func taskArguments(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = winArg(a)
	}
	return strings.Join(parts, " ")
}

// SchtasksXMLPath is where Install writes the definition: beside the wrapper,
// sharing its name with a .xml extension (mailarchive-<hash>.xml next to
// mailarchive-<hash>.cmd). Remove deletes it.
func SchtasksXMLPath(wrapperPath string) string {
	return strings.TrimSuffix(wrapperPath, ".cmd") + ".xml"
}

// nextOccurrence is the first instant at or after now on which a job at the
// given time-of-day fires: today if that time is still ahead, otherwise tomorrow
// (daily), the next Sunday (weekly), or the next hour (hourly, which uses only
// the minute). A fresh task therefore never has a StartBoundary in the past,
// which is what keeps Task Scheduler from treating the first fire as a missed
// start (FC13).
func nextOccurrence(now time.Time, iv Interval, hour, min int) time.Time {
	switch iv {
	case Hourly:
		t := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), min, 0, 0, now.Location())
		if !t.After(now) {
			t = t.Add(time.Hour)
		}
		return t
	case Weekly:
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		daysUntilSunday := (int(time.Sunday) - int(now.Weekday()) + 7) % 7
		t = t.AddDate(0, 0, daysUntilSunday)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t
	default: // Daily
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t
	}
}

// encodeUTF16LE returns s as UTF-16 little-endian bytes prefixed with a BOM.
func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+len(u)*2)
	b = append(b, 0xFF, 0xFE) // little-endian byte-order mark
	for _, r := range u {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

// decodeUTF16LE reverses encodeUTF16LE: it strips an optional little-endian BOM
// and decodes the remaining UTF-16LE code units to a UTF-8 string. Preview uses
// it to show the XML body in a terminal.
func decodeUTF16LE(b []byte) (string, error) {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	if len(b)%2 != 0 {
		return "", fmt.Errorf("UTF-16 data has an odd byte length (%d)", len(b))
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u)), nil
}
