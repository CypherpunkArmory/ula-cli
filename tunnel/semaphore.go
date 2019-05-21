// ulacli CLI used for interacting with holeulacli.io
// Copyright (C) 2018-2019  Orb.House, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package tunnel

import "sync/atomic"

type Semaphore struct {
	semaphore int32
}

func (l *Semaphore) CanRun() bool {
	return atomic.CompareAndSwapInt32(&l.semaphore, 0, 1)
}
func (l *Semaphore) Done() {
	atomic.CompareAndSwapInt32(&l.semaphore, 1, 0)
}
