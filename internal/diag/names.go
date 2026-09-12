package diag

import "fmt"

// APPL item ids and a few of their sids, by name. Protocol lore, marked
// inferred until a capture ties a name to what the GUI does with it. An
// id or sid not here is printed as its number.
var applIDs = map[byte]string{
	0x01: "SCRIPT", 0x02: "GRAPH", 0x03: "IXOS", 0x04: "ST_USER", 0x05: "DYNN", 0x06: "ST_R3INFO",
	0x07: "POPU", 0x08: "RFC_TR", 0x09: "DYNT", 0x0a: "CONTAINER", 0x0b: "MNUENTRY", 0x0c: "VARINFO",
	0x0e: "CONTROL", 0x0f: "UI_EVENT", 0x12: "ACC_LIST", 0x13: "RCUI", 0x14: "GUI_PACKET",
}

var applSIDs = map[byte]map[byte]string{
	0x04: {0x02: "CONNECT", 0x04: "SUPPORTDATA", 0x0b: "RFC_PARENT_UUID", 0x0d: "GUI_SESSION_UUID"},
	0x06: {0x02: "CODEPAGE", 0x03: "FLOATFORMAT", 0x0a: "USERNAME", 0x0b: "CLIENT", 0x11: "CODEPAGE_APP", 0x13: "TRANSACTION", 0x14: "ACCEL_LEGEND", 0x16: "SESSIONICON", 0x1f: "GUI_LABEL", 0x21: "DBNAME", 0x22: "CPUNAME", 0x23: "SYSNAME", 0x24: "SYSID"},
	0x09: {0x01: "DYNT_FOCUS", 0x02: "DYNT_ATOM", 0x03: "DYNT_EVENT_UNKNOWN", 0x04: "TABLE_ROW_REFERENCE", 0x05: "TABLE_ROW_DAT", 0x0f: "TABSTRIP_DEF", 0x10: "TABSTRIP_TAB"},
	0x0e: {0x01: "CONTROL_PROPERTIES", 0x02: "CONTROL_FOCUS", 0x03: "CONTROL_EVENT"},
	0x0f: {0x01: "UI_EVENT_SOURCE"},
}

// Name is the id/sid pair in words where a name is known.
func applName(id, sid byte) string {
	idName, ok := applIDs[id]
	if !ok {
		return fmt.Sprintf("%02x/%02x", id, sid)
	}
	if sidName, ok := applSIDs[id][sid]; ok {
		return idName + "." + sidName
	}
	return fmt.Sprintf("%s.%02x", idName, sid)
}
