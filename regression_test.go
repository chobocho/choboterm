package main

// Regression tests that pin down behavior the web mode (web*.go) must not
// change for the desktop window: which methods Wails binds, the shape of the
// events the page listens to, and how prompts, tabs and the close button work.

import (
	"bufio"
	"encoding/base64"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The methods Wails binds are exactly the exported methods of *App, and the
// generated frontend bindings (wailsjs) list the same ones. A refactor that
// promotes an extra exported method (e.g. from an embedded struct) would
// silently add a binding; one that drops a method would break the page.
func TestBoundMethodsMatchGeneratedBindings(t *testing.T) {
	f, err := os.Open("frontend/wailsjs/go/main/App.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	re := regexp.MustCompile(`^export function (\w+)\(`)
	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := re.FindStringSubmatch(sc.Text()); m != nil {
			want = append(want, m[1])
		}
	}
	var got []string
	typ := reflect.TypeOf(&App{})
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bound methods differ from wailsjs\n got: %v\nwant: %v", got, want)
	}
}

// term:data carries the tab id and the output as a base64 string; the page
// (main.ts) decodes exactly that.
func TestTermDataPayloadIsTabIDAndBase64(t *testing.T) {
	type ev struct {
		name string
		data []interface{}
	}
	got := make(chan ev, 16)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) { got <- ev{name, data} }
	sess := newPipeSession()
	tb := a.getTab(7)
	tb.sess = sess
	go tb.pump(sess)
	defer sess.out.Close()

	sess.out.Write([]byte("hi\x1b[0m"))
	select {
	case e := <-got:
		if e.name != "term:data" || len(e.data) != 2 {
			t.Fatalf("event %s %v", e.name, e.data)
		}
		if id, ok := e.data[0].(int); !ok || id != 7 {
			t.Fatalf("tab id %#v", e.data[0])
		}
		s, ok := e.data[1].(string)
		if !ok {
			t.Fatalf("payload is %T, not a base64 string", e.data[1])
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil || string(b) != "hi\x1b[0m" {
			t.Fatalf("payload %q (%v)", b, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no term:data")
	}
}

// Every tab event starts with the tab id, followed by the event's own values.
func TestTabEmitPrependsTabID(t *testing.T) {
	var got []interface{}
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) { got = append([]interface{}{name}, data...) }
	a.getTab(3).emit("log:changed", "/tmp/x.log")
	if !reflect.DeepEqual(got, []interface{}{"log:changed", 3, "/tmp/x.log"}) {
		t.Fatalf("emitted %v", got)
	}
}

// Status lines printed into the terminal are term:data events too (yellow).
func TestTermMessageIsYellowTermData(t *testing.T) {
	var name string
	var data []interface{}
	a := NewApp()
	a.hooks.emit = func(n string, d ...interface{}) { name, data = n, d }
	a.getTab(2).termMessage("done")
	b, _ := base64.StdEncoding.DecodeString(data[1].(string))
	if name != "term:data" || data[0] != 2 || string(b) != "\r\n\x1b[33mdone\x1b[0m\r\n" {
		t.Fatalf("%s %v %q", name, data, b)
	}
}

// A prompt is shown with auth:prompt (carrying its tab) and answered with
// AnswerPrompt; a cancel or a wrong number of values counts as cancelled.
func TestPromptRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		ok     bool
		want   []string
		wantOK bool
	}{
		{"answered", []string{"123456"}, true, []string{"123456"}, true},
		{"cancelled", nil, false, nil, false},
		{"wrong count", []string{"a", "b"}, true, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shown := make(chan Prompt, 1)
			a := NewApp()
			a.hooks.emit = func(name string, data ...interface{}) {
				if name == "auth:prompt" {
					shown <- data[0].(Prompt)
				}
			}
			go func() {
				p := <-shown
				a.AnswerPrompt(p.ID, c.values, c.ok)
			}()
			vals, ok := a.ask(5, "OTP", "code?", []PromptField{{Label: "Code"}})
			if ok != c.wantOK || !reflect.DeepEqual(vals, c.want) {
				t.Fatalf("got %v %v", vals, ok)
			}
		})
	}
}

// The prompt event carries the tab it belongs to, so the page shows it there.
func TestPromptCarriesTab(t *testing.T) {
	shown := make(chan Prompt, 1)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "auth:prompt" {
			shown <- data[0].(Prompt)
		}
	}
	go a.choose(9, "t", "m", "yes", "no")
	p := <-shown
	if p.Tab != 9 || p.OK != "yes" || p.Cancel != "no" || len(p.Fields) != 0 {
		t.Fatalf("prompt %+v", p)
	}
	a.AnswerPrompt(p.ID, nil, false)
}

// An unanswered prompt times out as cancelled and tells the page to close it.
func TestPromptTimeout(t *testing.T) {
	old := promptTimeout
	promptTimeout = 50 * time.Millisecond
	defer func() { promptTimeout = old }()
	var mu sync.Mutex
	var names []string
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		mu.Lock()
		names = append(names, name)
		mu.Unlock()
	}
	if _, ok := a.ask(1, "t", "m", []PromptField{{Label: "x"}}); ok {
		t.Fatal("timed out prompt answered")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(names, ",") != "auth:prompt,auth:promptDone" {
		t.Fatalf("events %v", names)
	}
}

// Tab ids are whatever the page chose, including large ones; tabs are
// created on first use and forgotten when closed.
func TestTabIDsComeFromThePage(t *testing.T) {
	a := NewApp()
	big := 5<<20 + 1
	if a.findTab(big) != nil {
		t.Fatal("tab exists before use")
	}
	tb := a.getTab(big)
	if tb.id != big || a.getTab(big) != tb {
		t.Fatal("getTab not stable")
	}
	a.CloseTab(big)
	if a.findTab(big) != nil {
		t.Fatal("tab not forgotten")
	}
	// Calls for unknown tabs are ignored, not errors or panics.
	a.Send(12345, "x")
	a.Resize(12345, 80, 24)
	a.Disconnect(12345)
}

// GetVersion is what the window title and the update check use.
func TestGetVersion(t *testing.T) {
	if NewApp().GetVersion() != AppVersion {
		t.Fatal("version")
	}
}
