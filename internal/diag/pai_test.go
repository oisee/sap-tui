package diag

import "testing"

func TestClientFields(t *testing.T) {
	// A client echo: two input atoms at (1,9) and (2,9).
	atoms := []Atom{
		InputField(1, 9, 10, "42"),
		FieldName(1, 9, "P_NUM", AttrYes3D),
		InputField(2, 9, 10, "hello"),
		FieldName(2, 9, "P_TXT", AttrYes3D),
	}
	items := []Item{{Type: ItemAPPL4, ID: 0x09, SID: 0x02, Value: EncodeDyntAtoms(atoms)}}
	fv := ClientFields(items)
	if len(fv) != 2 || fv[0].Row != 1 || fv[0].Col != 9 || fv[0].Value != "42" || fv[1].Value != "hello" {
		t.Fatalf("fields: %+v", fv)
	}
}

func TestEvents(t *testing.T) {
	xml := `<?xml version="1.0" encoding="sap*"?><DATAMANAGER> <EVENTS>  <EVENT SHELLID ="160" EVENTID ="14" SHELLEVENT ="X">   <PARAM PID ="0" VALUE ="WB_ACTIVATE"/></EVENT></EVENTS></DATAMANAGER>`
	ev := Events([]Item{{Type: ItemXML, Value: []byte(xml)}})
	if len(ev) != 1 || ev[0].ShellID != "160" || ev[0].EventID != "14" || ev[0].Value != "WB_ACTIVATE" {
		t.Fatalf("events: %+v", ev)
	}
}
