/*** Copyright (c) 2016, University of Florida Research Foundation, Inc. ***
 *** For more information please refer to the LICENSE.md file            ***/

// Package gorods is a Golang binding for the iRods C API (iRods client library).
// GoRods uses cgo to call iRods client functions.
package gorods

// #cgo CFLAGS: -ggdb -I/usr/include/irods -I${SRCDIR}/lib/include
// #cgo LDFLAGS: -L${SRCDIR}/lib/build -lgorods /usr/lib/libirods_client.a /usr/lib/libirods_client_api.a /usr/lib/irods/externals/libboost_thread.a /usr/lib/irods/externals/libboost_system.a /usr/lib/irods/externals/libboost_filesystem.a /usr/lib/irods/externals/libboost_program_options.a /usr/lib/irods/externals/libboost_chrono.a /usr/lib/irods/externals/libboost_regex.a /usr/lib/irods/externals/libjansson.a /usr/lib/libirods_client_core.a /usr/lib/libirods_client_plugins.a -lz -lssl -lcrypto -ldl -lpthread -lm -lrt -lstdc++ -rdynamic -Wno-write-strings -DBOOST_SYSTEM_NO_DEPRECATED
// #include "wrapper.h"
import "C"

import (
	"fmt"
	"unsafe"
)

// EnvironmentDefined and UserDefined constants are used when calling
// gorods.New(ConnectionOptions{ Type: ... })
// When EnvironmentDefined is specified, the options stored in ~/.irods/irods_environment.json will be used.
// When UserDefined is specified you must also pass Host, Port, Username, and Zone.
// Password should be set regardless.
const (
	EnvironmentDefined = iota
	UserDefined
)

// Used when calling Type() on different gorods objects
const (
	DataObjType = iota
	CollectionType
	ResourceType
	ResourceGroupType
	UserType
)

func getTypeString(t int) string {
	switch t {
		case DataObjType:
			return "d"
		case CollectionType:
			return "C"
		case ResourceType:
			return "R"
		case UserType:
			return "u"
		default:
			panic(newError(Fatal, "unrecognized meta type constant"))
	}
}

// IRodsObj is a generic interface used to detect the object type and access common fields
type IRodsObj interface {
	GetType() int
	GetName() string
	GetPath() string
	GetCol() *Collection
	GetCon() *Connection

	Meta() (*MetaCollection, error)
	Attribute(string) (*Meta, error)
	AddMeta(Meta) (*Meta, error)
	DeleteMeta(string) (*MetaCollection, error)

	String() string
	Open() error
	Close() error
}

type IRodsObjs []IRodsObj

// Exists checks to see if a collection exists in the slice
// and returns true or false
func (objs IRodsObjs) Exists(path string) bool {
	if o := objs.Find(path); o != nil {
		return true
	}

	return false
}

// Find gets a collection from the slice and returns nil if one is not found.
// Both the collection name or full path can be used as input.
func (objs IRodsObjs) Find(path string) IRodsObj {
	
	// Strip trailing forward slash
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	for i, obj := range objs {
		if obj.GetPath() == path || obj.GetName() == path {
			return objs[i]
		}
	}

	return nil
}

// FindRecursive acts just like Find, but also searches sub collections recursively.
// If the collection was not explicitly loaded recursively, only the first level of sub collections will be searched.
func (objs IRodsObjs) FindRecursive(path string) IRodsObj {
	
	// Strip trailing forward slash
	if path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	for i, obj := range objs {
		if obj.GetPath() == path || obj.GetName() == path {
			return objs[i]
		}


		if obj.GetType() == CollectionType {

			col := obj.(*Collection)

			if col.IsRecursive() {
				if obs, err := col.GetCollections(); err == nil {
					// Use Collections() since we already loaded everything
					if subCol := obs.FindRecursive(path); subCol != nil {
						return subCol
					}
				}
			} else {

				// Use DataObjects so we don't load new collections (don't init)
				var filtered IRodsObjs

				for n, o := range col.DataObjects {
					if o.GetType() == CollectionType {
						filtered = append(filtered, col.DataObjects[n])
					}
				}

				if subCol := filtered.FindRecursive(path); subCol != nil {
					return subCol
				}

			}
		}
		
	}

	return nil
}


// ConnectionOptions are used when creating iRods iCAT server connections see gorods.New() docs for more info.
type ConnectionOptions struct {
	Type int

	Host string
	Port int
	Zone string

	Username string
	Password string
}

type Connection struct {
	ccon *C.rcComm_t

	Connected   bool
	Options     *ConnectionOptions
	OpenedObjs  IRodsObjs
}


// New creates a connection to an iRods iCAT server. EnvironmentDefined and UserDefined
// constants are used in ConnectionOptions{ Type: ... }).
// When EnvironmentDefined is specified, the options stored in ~/.irods/irods_environment.json will be used.
// When UserDefined is specified you must also pass Host, Port, Username, and Zone. Password
// should be set regardless.
func New(opts ConnectionOptions) (*Connection, error) {
	con := new(Connection)

	con.Options = &opts

	var (
		status   C.int
		errMsg   *C.char
		password *C.char
	)

	if con.Options.Password != "" {
		password = C.CString(con.Options.Password)

		defer C.free(unsafe.Pointer(password))
	}

	// Are we passing env values?
	if con.Options.Type == UserDefined {
		host := C.CString(con.Options.Host)
		port := C.int(con.Options.Port)
		username := C.CString(con.Options.Username)
		zone := C.CString(con.Options.Zone)

		defer C.free(unsafe.Pointer(host))
		defer C.free(unsafe.Pointer(username))
		defer C.free(unsafe.Pointer(zone))

		// BUG(jjacquay712): iRods C API code outputs errors messages, need to implement connect wrapper (gorods_connect_env) from a lower level to suppress this output
		// https://github.com/irods/irods/blob/master/iRODS/lib/core/src/rcConnect.cpp#L109
		status = C.gorods_connect_env(&con.ccon, host, port, username, zone, password, &errMsg)
	} else {
		status = C.gorods_connect(&con.ccon, password, &errMsg)
	}

	if status == 0 {
		con.Connected = true
	} else {
		return nil, newError(Fatal, fmt.Sprintf("iRods Connect Failed: %v", C.GoString(errMsg)))
	}

	return con, nil
}

// Disconnect closes connection to iRods iCAT server, returns error on failure or nil on success
func (con *Connection) Disconnect() error {
	if status := int(C.rcDisconnect(con.ccon)); status < 0 {
		return newError(Fatal, fmt.Sprintf("iRods rcDisconnect Failed"))
	}

	con.Connected = false

	return nil
}

// String provides connection status and options provided during initialization (gorods.New)
func (obj *Connection) String() string {

	if obj.Options.Type == UserDefined {
		return fmt.Sprintf("Host: %v@%v:%v/%v, Connected: %v\n", obj.Options.Username, obj.Options.Host, obj.Options.Port, obj.Options.Zone, obj.Connected)
	}

	var (
		username *C.char
		host     *C.char
		port     C.int
		zone     *C.char
	)

	defer C.free(unsafe.Pointer(username))
	defer C.free(unsafe.Pointer(host))
	defer C.free(unsafe.Pointer(zone))

	if status := C.irods_env(&username, &host, &port, &zone); status != 0 {
		panic(newError(Fatal, fmt.Sprintf("iRods getEnv Failed")))
	}

	return fmt.Sprintf("Host: %v@%v:%v/%v, Connected: %v\n", C.GoString(username), C.GoString(host), int(port), C.GoString(zone), obj.Connected)
}

// Collection initializes and returns an existing iRods collection using the specified path
func (con *Connection) Collection(startPath string, recursive bool) (*Collection, error) {

	// Check the cache
	if collection := con.OpenedObjs.FindRecursive(startPath); collection == nil {

		// Load collection, no cache found
		if col, err := getCollection(startPath, recursive, con); err == nil {
			con.OpenedObjs = append(con.OpenedObjs, col)

			return col, nil
		} else {
			return nil, err
		}
	} else {
		return collection.(*Collection), nil
	}
}

// DataObject directly returns a specific DataObj without the need to traverse collections. Must pass full path of data object.
func (con *Connection) DataObject(dataObjPath string) (dataobj *DataObj, err error) {
	// We use the caching mechanism from Collection()
	dataobj, err = getDataObj(dataObjPath, con)
	
	return
}

// SearchDataObjects searchs for and returns DataObjs slice based on a search string. Use '%' as a wildcard. Equivalent to ilocate command
func (con *Connection) SearchDataObjects(dataObjPath string) (dataobj *DataObj, err error) {
	return nil, nil
}

// SearchDataObjects searchs for and returns DataObjs slice based on a search string. Use '%' as a wildcard. Equivalent to ilocate command
func (con *Connection) QueryMeta(dataObjPath string) (dataobj *DataObj, err error) {
	return nil, nil
}

// func (con *Connection) QueryMeta(query string) (collection *Collection, err error) {

// }
