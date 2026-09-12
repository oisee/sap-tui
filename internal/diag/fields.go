package diag

import "strings"

// A screen's fields carry their ABAP name in the name atom that follows
// each one. To change a field's value without disturbing the rest of a
// real screen — so its dynpro definition still matches and the client
// still submits it — read the atoms, set a value by name, and write them
// back. This keeps every position, attribute and name the server sent.

// FieldIndex pairs each field atom (input, output, checkbox, radio) with
// the ABAP name in the name atom right after it, returning the parsed
// atoms and name -> index of the field atom.
func FieldIndex(value []byte) ([]Atom, map[string]int) {
	atoms, _ := ParseDyntAtoms(value)
	byName := map[string]int{}
	last := -1
	for i, a := range atoms {
		switch a.EType {
		case AtomInputField, AtomOutputField, AtomCheckbox, AtomRadioButton:
			last = i
		case AtomFieldName:
			if last >= 0 {
				byName[strings.ToUpper(a.Value())] = last
				last = -1
			}
		}
	}
	return atoms, byName
}

// SetField sets a field's value by name, padding it to the field's width
// the way SAP holds it — left for a right-justified number, right
// otherwise. A name the screen does not have is ignored.
func SetField(atoms []Atom, byName map[string]int, name, value string) {
	i, ok := byName[strings.ToUpper(name)]
	if !ok {
		return
	}
	a := &atoms[i]
	w := a.VisibleLength
	if w <= 0 {
		w = len(value)
	}
	if len(value) < w {
		pad := strings.Repeat(" ", w-len(value))
		if a.Attr&AttrJustRight != 0 {
			value = pad + value
		} else {
			value = value + pad
		}
	}
	a.Text = value
	a.Length = len(value)
}
