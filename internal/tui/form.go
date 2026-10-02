package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// form is a small modal of fields: text inputs, and choices cycled with
// left and right so nothing with a fixed set of answers needs typing.
//
// It stays open on error. The store's message is shown inside it and what
// was typed is kept, because a form that closes and drops your input on a
// typo is worse than having no form.
type form struct {
	title  string
	fields []*field
	focus  int
	err    string
	submit func(values map[string]string) (status string, err error)
}

type field struct {
	key      string
	label    string
	hint     string
	input    textinput.Model
	choices  []string // when set, a choice cycled with left and right
	labels   []string // what each choice shows, if not the choice itself
	choice   int
	optional bool
}

func textField(key, label, hint, value string) *field {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 300
	in.SetValue(value)
	in.CursorEnd()
	return &field{key: key, label: label, hint: hint, input: in}
}

func choiceField(key, label string, choices, labels []string, selected int) *field {
	return &field{key: key, label: label, choices: choices, labels: labels, choice: selected}
}

func (f *field) value() string {
	if f.choices != nil {
		if len(f.choices) == 0 {
			return ""
		}
		return f.choices[f.choice]
	}
	return strings.TrimSpace(f.input.Value())
}

func (f *field) shown() string {
	if f.labels != nil && f.choice < len(f.labels) {
		return f.labels[f.choice]
	}
	return f.value()
}

func newForm(title string, submit func(map[string]string) (string, error), fields ...*field) *form {
	fm := &form{title: title, fields: fields, submit: submit}
	fm.focusField(0)
	return fm
}

func (fm *form) focusField(i int) {
	for j, f := range fm.fields {
		if f.choices == nil {
			if j == i {
				f.input.Focus()
			} else {
				f.input.Blur()
			}
		}
	}
	fm.focus = i
}

func (fm *form) values() map[string]string {
	out := map[string]string{}
	for _, f := range fm.fields {
		out[f.key] = f.value()
	}
	return out
}

// update handles a key. done is true when the form should close: submitted
// successfully, or cancelled, in which case status says which.
func (fm *form) update(msg tea.KeyMsg) (done bool, status string, cmd tea.Cmd) {
	f := fm.fields[fm.focus]
	switch msg.String() {
	case "esc":
		return true, "Cancelled", nil
	case "tab", "down":
		fm.focusField((fm.focus + 1) % len(fm.fields))
		return false, "", nil
	case "shift+tab", "up":
		fm.focusField((fm.focus + len(fm.fields) - 1) % len(fm.fields))
		return false, "", nil
	case "enter":
		if fm.focus < len(fm.fields)-1 {
			fm.focusField(fm.focus + 1)
			return false, "", nil
		}
		return fm.send()
	case "ctrl+s":
		return fm.send()
	}
	if f.choices != nil {
		switch msg.String() {
		case "left", "h":
			f.choice = (f.choice + len(f.choices) - 1) % max(len(f.choices), 1)
		case "right", "l", " ":
			f.choice = (f.choice + 1) % max(len(f.choices), 1)
		}
		return false, "", nil
	}
	f.input, cmd = f.input.Update(msg)
	fm.err = ""
	return false, "", cmd
}

func (fm *form) send() (bool, string, tea.Cmd) {
	for i, f := range fm.fields {
		if !f.optional && f.value() == "" {
			fm.err = f.label + " is needed"
			fm.focusField(i)
			return false, "", nil
		}
	}
	status, err := fm.submit(fm.values())
	if err != nil {
		fm.err = err.Error()
		return false, "", nil
	}
	return true, status, nil
}

func (fm *form) view(width int) string {
	labelW := 0
	for _, f := range fm.fields {
		labelW = max(labelW, len(f.label))
	}
	var b strings.Builder
	b.WriteString(styleBold.Render(fm.title) + "\n\n")
	for i, f := range fm.fields {
		label := pad(f.label, labelW)
		if i == fm.focus {
			label = styleKey.Render(label)
		} else {
			label = styleDim.Render(label)
		}
		var value string
		switch {
		case f.choices != nil && len(f.choices) == 0:
			value = styleDim.Render("(none)")
		case f.choices != nil:
			value = "‹ " + f.shown() + " ›"
			if i == fm.focus {
				value = styleAccent.Render(value)
			}
		default:
			value = f.input.View()
		}
		line := "  " + label + "  " + value
		if f.hint != "" && i == fm.focus {
			line += "  " + styleHint.Render(f.hint)
		}
		b.WriteString(truncate(line, width) + "\n")
	}
	b.WriteString("\n")
	if fm.err != "" {
		b.WriteString("  " + styleUrgent.Render(fm.err) + "\n\n")
	}
	b.WriteString("  " + styleHint.Render("tab next · ←/→ choose · enter on the last field saves · ctrl+s saves · esc cancels"))
	return b.String()
}
