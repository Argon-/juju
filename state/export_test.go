// launchpad.net/juju/state
//
// Copyright (c) 2011-2012 Canonical Ltd.
package state

import (
	"launchpad.net/gozk/zookeeper"
)

// ZkAddr is the address for the connection to the server.
var ZkAddr string

// ZkConn returns the ZooKeeper connection used by a state.
// It is defined in export_test.go so that tests can have access
// to this connection.
func ZkConn(st *State) *zookeeper.Conn {
	return st.zk
}
