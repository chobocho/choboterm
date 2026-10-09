package main

import (
	"sync"
	"time"
)

// PromptField is one input in a prompt: a label and whether to hide what is typed.
type PromptField struct {
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
}

// Prompt asks the user for one or more values while connecting
// (a key passphrase, a one-time code...). The page answers with AnswerPrompt.
type Prompt struct {
	ID      int           `json:"id"`
	Tab     int           `json:"tab"`
	Title   string        `json:"title"`
	Message string        `json:"message"`
	Fields  []PromptField `json:"fields"`
	OK      string        `json:"ok"`     // button labels; "" = 확인 / 취소
	Cancel  string        `json:"cancel"` // (a question with no fields is a choice of two)
}

type promptAnswer struct {
	values []string
	ok     bool
}

// asker asks the user for the fields' values; ok is false if they cancelled.
type asker func(title, message string, fields []PromptField) (values []string, ok bool)

// promptTimeout is how long a prompt waits for the user before it counts as cancelled.
var promptTimeout = 5 * time.Minute

var prompts = struct {
	sync.Mutex
	next    int
	pending map[int]chan promptAnswer
}{pending: map[int]chan promptAnswer{}}

// ask shows a prompt for tab tabID in the page and waits for the answer.
func (a *App) ask(tabID int, title, message string, fields []PromptField) ([]string, bool) {
	return a.askPrompt(Prompt{Tab: tabID, Title: title, Message: message, Fields: fields})
}

// choose asks a question with two answers; true means the ok one. Closing the
// window or not answering in time counts as cancel.
func (a *App) choose(tabID int, title, message, ok, cancel string) bool {
	_, yes := a.askPrompt(Prompt{Tab: tabID, Title: title, Message: message, OK: ok, Cancel: cancel})
	return yes
}

func (a *App) askPrompt(p Prompt) ([]string, bool) {
	fields := p.Fields
	if a.hooks.prompt != nil {
		return a.hooks.prompt(p)
	}
	ch := make(chan promptAnswer, 1)
	prompts.Lock()
	prompts.next++
	p.ID = prompts.next
	prompts.pending[p.ID] = ch
	prompts.Unlock()
	defer func() {
		prompts.Lock()
		delete(prompts.pending, p.ID)
		prompts.Unlock()
	}()

	a.emit("auth:prompt", p)
	select {
	case ans := <-ch:
		if !ans.ok || len(ans.values) != len(fields) {
			return nil, false
		}
		return ans.values, true
	case <-time.After(promptTimeout):
		a.emit("auth:promptDone", p.ID) // close the dialog in the page
		return nil, false
	}
}

// asker returns an asker bound to a tab.
func (a *App) asker(tabID int) asker {
	return func(title, message string, fields []PromptField) ([]string, bool) {
		return a.ask(tabID, title, message, fields)
	}
}

// AnswerPrompt delivers the user's answer to the prompt with the given id.
func (a *App) AnswerPrompt(id int, values []string, ok bool) {
	prompts.Lock()
	ch := prompts.pending[id]
	prompts.Unlock()
	if ch != nil {
		select {
		case ch <- promptAnswer{values: values, ok: ok}:
		default:
		}
	}
}
