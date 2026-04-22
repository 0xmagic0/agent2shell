package session

import "bufio"

// readLoop is the sole reader of the TCP connection. It runs in its own
// goroutine from the moment New returns until the connection is closed.
//
// For each line scanned:
//   - If an Exec is active (execCh != nil), the line is forwarded to that
//     channel. A select with doneCh prevents blocking on a full buffer
//     during shutdown.
//   - Otherwise, onOutput is called (if non-nil).
//
// On scanner error or EOF, doneCh is closed exactly once via doneOnce.
func (s *Session) readLoop() {
	scanner := bufio.NewScanner(s.conn)
	for scanner.Scan() {
		line := scanner.Text()

		s.chMu.RLock()
		ch := s.execCh
		s.chMu.RUnlock()

		if ch != nil {
			select {
			case ch <- line:
			case <-s.doneCh:
				return
			}
		} else if s.onOutput != nil {
			s.onOutput(line)
		}
	}
	// EOF or read error — signal session shutdown.
	s.doneOnce.Do(func() { close(s.doneCh) })
}
