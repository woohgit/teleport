/*
Copyright 2015 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gravitational/teleport/lib/backend/boltbk"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/mailgun/timetools"
	. "gopkg.in/check.v1"
)

func TestSessions(t *testing.T) { TestingT(t) }

type BoltSuite struct {
	dir   string
	srv   *server
	bk    *boltbk.BoltBackend
	clock *timetools.FreezedTime
}

var _ = Suite(&BoltSuite{})

func (s *BoltSuite) SetUpSuite(c *C) {
	utils.InitLoggerForTests()
}

func (s *BoltSuite) SetUpTest(c *C) {
	s.clock = &timetools.FreezedTime{
		CurrentTime: time.Date(2016, 9, 8, 7, 6, 5, 0, time.UTC),
	}
	s.dir = c.MkDir()

	var err error
	s.bk, err = boltbk.New(filepath.Join(s.dir, "db"), boltbk.Clock(s.clock))
	c.Assert(err, IsNil)

	srv, err := New(s.bk, Clock(s.clock))
	s.srv = srv.(*server)
	c.Assert(err, IsNil)
}

func (s *BoltSuite) TearDownTest(c *C) {
	c.Assert(s.bk.Close(), IsNil)
}

func (s *BoltSuite) TestID(c *C) {
	id := NewID()
	id2, err := ParseID(id.String())
	c.Assert(err, IsNil)
	c.Assert(id, Equals, *id2)

	for _, val := range []string{"garbage", "", "   ", string(id) + "extra"} {
		id := ID(val)
		c.Assert(id.Check(), NotNil)
	}
}

func (s *BoltSuite) TestSessionsCRUD(c *C) {
	out, err := s.srv.GetSessions()
	c.Assert(err, IsNil)
	c.Assert(len(out), Equals, 0)

	sess := Session{
		ID:             NewID(),
		Active:         true,
		TerminalParams: TerminalParams{W: 100, H: 100},
		Login:          "bob",
		LastActive:     s.clock.UtcNow(),
		Created:        s.clock.UtcNow(),
	}
	c.Assert(s.srv.CreateSession(sess), IsNil)

	out, err = s.srv.GetSessions()
	c.Assert(err, IsNil)
	c.Assert(out, DeepEquals, []Session{sess})

	s2, err := s.srv.GetSession(sess.ID)
	c.Assert(err, IsNil)
	c.Assert(s2, DeepEquals, &sess)

	// Mark session inactive
	err = s.srv.UpdateSession(UpdateRequest{
		ID:     sess.ID,
		Active: Bool(false),
	})
	c.Assert(err, IsNil)

	sess.Active = false
	s2, err = s.srv.GetSession(sess.ID)
	c.Assert(err, IsNil)
	c.Assert(s2, DeepEquals, &sess)

	// Update session terminal parameter
	err = s.srv.UpdateSession(UpdateRequest{
		ID:             sess.ID,
		TerminalParams: &TerminalParams{W: 101, H: 101},
	})
	c.Assert(err, IsNil)

	sess.TerminalParams = TerminalParams{W: 101, H: 101}
	s2, err = s.srv.GetSession(sess.ID)
	c.Assert(err, IsNil)
	c.Assert(s2, DeepEquals, &sess)
}

// TestSessionsInactivity makes sure that session will be marked
// as inactive after period of inactivity
func (s *BoltSuite) TestSessionsInactivity(c *C) {
	sess := Session{
		ID:             NewID(),
		Active:         true,
		TerminalParams: TerminalParams{W: 100, H: 100},
		Login:          "bob",
		LastActive:     s.clock.UtcNow(),
		Created:        s.clock.UtcNow(),
	}
	c.Assert(s.srv.CreateSession(sess), IsNil)

	// sleep to let it expire:
	s.clock.Sleep(defaults.ActiveSessionTTL + time.Second)

	// should not be in active sessions:
	s2, err := s.srv.GetSession(sess.ID)
	c.Assert(err, IsNil)
	c.Assert(s2, IsNil)
}

func (s *BoltSuite) TestPartiesCRUD(c *C) {
	// create session:
	sess := Session{
		ID:             NewID(),
		Active:         true,
		TerminalParams: TerminalParams{W: 100, H: 100},
		Login:          "vincent",
		LastActive:     s.clock.UtcNow(),
		Created:        s.clock.UtcNow(),
	}
	c.Assert(s.srv.CreateSession(sess), IsNil)
	// add two people:
	parties := []Party{
		{
			ID:         NewID(),
			RemoteAddr: "1_remote_addr",
			User:       "first",
			ServerID:   "luna",
			LastActive: s.clock.UtcNow(),
		},
		{
			ID:         NewID(),
			RemoteAddr: "2_remote_addr",
			User:       "second",
			ServerID:   "luna",
			LastActive: s.clock.UtcNow(),
		},
	}
	s.srv.UpdateSession(UpdateRequest{
		ID:      sess.ID,
		Parties: &parties,
	})
	// verify they're in the session:
	copy, err := s.srv.GetSession(sess.ID)
	c.Assert(err, IsNil)
	c.Assert(len(copy.Parties), Equals, 2)

	// empty update (list of parties must not change)
	s.srv.UpdateSession(UpdateRequest{ID: sess.ID})
	copy, _ = s.srv.GetSession(sess.ID)
	c.Assert(len(copy.Parties), Equals, 2)

	// remove the 2nd party:
	deleted := copy.RemoveParty(parties[1].ID)
	c.Assert(deleted, Equals, true)
	s.srv.UpdateSession(UpdateRequest{ID: copy.ID,
		Parties: &copy.Parties})
	copy, _ = s.srv.GetSession(sess.ID)
	c.Assert(len(copy.Parties), Equals, 1)

	// we still have the 1st party in:
	c.Assert(parties[0].ID, Equals, copy.Parties[0].ID)
}
