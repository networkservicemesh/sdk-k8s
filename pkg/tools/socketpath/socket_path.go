// Copyright (c) 2018-2020 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package socketpath provides unix socket path helping tools
package socketpath

import (
	"net"
	"os"

	"github.com/pkg/errors"
)

// SocketPath is a unix socket file path.
type SocketPath string

// Network returns "unix" socket type
func (socket SocketPath) Network() string {
	return "unix"
}

// String returns string representation
func (socket SocketPath) String() string {
	return string(socket)
}

// SocketCleanup check for the presence of a stale socket and if it finds it, removes it.
func SocketCleanup(socket SocketPath) error {
	fi, err := os.Stat(socket.String())
	if err == nil && (fi.Mode()&os.ModeSocket) != 0 {
		if err = os.Remove(socket.String()); err != nil {
			return errors.Wrapf(err, "cannot remove listen endpoint %s", socket.String())
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failure stat of socket file %s", socket.String())
	}
	return nil
}

var _ net.Addr = (*SocketPath)(nil)
