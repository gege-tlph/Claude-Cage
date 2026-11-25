package managedproxy

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsRASErrorBufferTooSmall = 603 // ERROR_BUFFER_TOO_SMALL in raserror.h

var rasEnumConnections = windows.NewLazySystemDLL("rasapi32.dll").NewProc("RasEnumConnectionsW")

// windowsRASConnection is RASCONNW. ras.h packs it to four bytes, including
// the pointer-sized HRASCONN.
type windowsRASConnection struct {
	size          uint32
	handle        [unsafe.Sizeof(uintptr(0)) / 4]uint32
	entryName     [257]uint16
	deviceType    [17]uint16
	deviceName    [129]uint16
	phonebook     [windows.MAX_PATH]uint16
	subEntry      uint32
	entryGUID     windows.GUID
	flags         uint32
	logonLUID     [2]uint32
	correlationID windows.GUID
}

// windowsPPPoEConnections names the active built-in PPPoE connections. Windows
// uses a connection's RAS entry name as its adapter name. Two entries with the
// same name are left out: the adapter could be either of them.
// From Sodugwuy's contribution in upstream #745 (tested on a real PPPoE line).
func windowsPPPoEConnections(ctx context.Context) map[string]bool {
	names := make(map[string]bool)
	connectionSize := uint32(unsafe.Sizeof(windowsRASConnection{}))
	connections := make([]windowsRASConnection, 1)
	for attempts := 0; attempts < 3 && ctx.Err() == nil; attempts++ {
		connections[0].size = connectionSize
		size := uint32(len(connections)) * connectionSize
		var count uint32
		code, _, _ := rasEnumConnections.Call(
			uintptr(unsafe.Pointer(&connections[0])),
			uintptr(unsafe.Pointer(&size)),
			uintptr(unsafe.Pointer(&count)),
		)
		if code == windowsRASErrorBufferTooSmall && size > uint32(len(connections))*connectionSize {
			connections = make([]windowsRASConnection, (size-1)/connectionSize+1)
			continue
		}
		if code != 0 || count > uint32(len(connections)) || ctx.Err() != nil {
			break
		}
		for _, conn := range connections[:count] {
			name := windows.UTF16ToString(conn.entryName[:])
			_, duplicate := names[name]
			names[name] = name != "" && !duplicate && windows.UTF16ToString(conn.deviceType[:]) == "PPPoE"
		}
		break
	}
	return names
}
