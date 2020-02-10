// +build linux,cgo,!static_build,journald

package journald

// #include <sys/types.h>
// #include <sys/poll.h>
// #include <systemd/sd-journal.h>
// #include <errno.h>
// #include <stdio.h>
// #include <stdlib.h>
// #include <string.h>
// #include <time.h>
// #include <unistd.h>
//
//static int get_message(sd_journal *j, const char **msg, size_t *length, int *partial)
//{
//	int rc;
//	size_t plength;
//	*msg = NULL;
//	*length = 0;
//	plength = strlen("CONTAINER_PARTIAL_MESSAGE=true");
//	rc = sd_journal_get_data(j, "CONTAINER_PARTIAL_MESSAGE", (const void **) msg, length);
//	*partial = ((rc == 0) && (*length == plength) && (memcmp(*msg, "CONTAINER_PARTIAL_MESSAGE=true", plength) == 0));
//	rc = sd_journal_get_data(j, "MESSAGE", (const void **) msg, length);
//	if (rc == 0) {
//		if (*length > 8) {
//			(*msg) += 8;
//			*length -= 8;
//		} else {
//			*msg = NULL;
//			*length = 0;
//			rc = -ENOENT;
//		}
//	}
//	return rc;
//}
//static int get_priority(sd_journal *j, int *priority)
//{
//	const void *data;
//	size_t i, length;
//	int rc;
//	*priority = -1;
//	rc = sd_journal_get_data(j, "PRIORITY", &data, &length);
//	if (rc == 0) {
//		if ((length > 9) && (strncmp(data, "PRIORITY=", 9) == 0)) {
//			*priority = 0;
//			for (i = 9; i < length; i++) {
//				*priority = *priority * 10 + ((const char *)data)[i] - '0';
//			}
//			if (length > 9) {
//				rc = 0;
//			}
//		}
//	}
//	return rc;
//}
//static int is_attribute_field(const char *msg, size_t length)
//{
//	static const struct known_field {
//		const char *name;
//		size_t length;
//	} fields[] = {
//		{"MESSAGE", sizeof("MESSAGE") - 1},
//		{"MESSAGE_ID", sizeof("MESSAGE_ID") - 1},
//		{"PRIORITY", sizeof("PRIORITY") - 1},
//		{"CODE_FILE", sizeof("CODE_FILE") - 1},
//		{"CODE_LINE", sizeof("CODE_LINE") - 1},
//		{"CODE_FUNC", sizeof("CODE_FUNC") - 1},
//		{"ERRNO", sizeof("ERRNO") - 1},
//		{"SYSLOG_FACILITY", sizeof("SYSLOG_FACILITY") - 1},
//		{"SYSLOG_IDENTIFIER", sizeof("SYSLOG_IDENTIFIER") - 1},
//		{"SYSLOG_PID", sizeof("SYSLOG_PID") - 1},
//		{"CONTAINER_NAME", sizeof("CONTAINER_NAME") - 1},
//		{"CONTAINER_ID", sizeof("CONTAINER_ID") - 1},
//		{"CONTAINER_ID_FULL", sizeof("CONTAINER_ID_FULL") - 1},
//		{"CONTAINER_TAG", sizeof("CONTAINER_TAG") - 1},
//	};
//	unsigned int i;
//	void *p;
//	if ((length < 1) || (msg[0] == '_') || ((p = memchr(msg, '=', length)) == NULL)) {
//		return -1;
//	}
//	length = ((const char *) p) - msg;
//	for (i = 0; i < sizeof(fields) / sizeof(fields[0]); i++) {
//		if ((fields[i].length == length) && (memcmp(fields[i].name, msg, length) == 0)) {
//			return -1;
//		}
//	}
//	return 0;
//}
//static int get_attribute_field(sd_journal *j, const char **msg, size_t *length)
//{
//	int rc;
//	*msg = NULL;
//	*length = 0;
//	while ((rc = sd_journal_enumerate_data(j, (const void **) msg, length)) > 0) {
//		if (is_attribute_field(*msg, *length) == 0) {
//			break;
//		}
//		rc = -ENOENT;
//	}
//	return rc;
//}
//static int wait_for_data_cancelable(sd_journal *j, int pipefd)
//{
//	struct pollfd fds[2];
//	uint64_t when = 0;
//	int timeout, jevents, i;
//	struct timespec ts;
//	uint64_t now;
//
//	memset(&fds, 0, sizeof(fds));
//	fds[0].fd = pipefd;
//	fds[0].events = POLLHUP;
//	fds[1].fd = sd_journal_get_fd(j);
//	if (fds[1].fd < 0) {
//		return fds[1].fd;
//	}
//
//	do {
//		jevents = sd_journal_get_events(j);
//		if (jevents < 0) {
//			return jevents;
//		}
//		fds[1].events = jevents;
//		sd_journal_get_timeout(j, &when);
//		if (when == -1) {
//			timeout = -1;
//		} else {
//			clock_gettime(CLOCK_MONOTONIC, &ts);
//			now = (uint64_t) ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
//			timeout = when > now ? (int) ((when - now + 999) / 1000) : 0;
//		}
//		i = poll(fds, 2, timeout);
//		if ((i == -1) && (errno != EINTR)) {
//			/* An unexpected error. */
//			return (errno != 0) ? -errno : -EINTR;
//		}
//		if (fds[0].revents & POLLHUP) {
//			/* The close notification pipe was closed. */
//			return 0;
//		}
//		switch (sd_journal_process(j)) {
//		case SD_JOURNAL_APPEND:
//		case SD_JOURNAL_INVALIDATE:
//			/* Data, which we might care about, was appended. */
//			return 1;
//		}
//	} while ((fds[0].revents & POLLHUP) == 0);
//	return 0;
//}
import "C"

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/journal"
	"github.com/docker/docker/daemon/logger"
)

func (s *journald) Close() error {
	s.readers.mu.Lock()
	s.closeWriter()
	for reader := range s.readers.readers {
		reader.Close()
	}
	s.readers.mu.Unlock()
	return nil
}

func (s *journald) drainJournal(logWatcher *logger.LogWatcher, config logger.ReadConfig, j *C.sd_journal, oldCursor *C.char) (*C.char, bool) {
	var msg, data, cursor *C.char
	var length C.size_t
	var stamp C.uint64_t
	var priority, partial C.int

	// Give the journal handle an opportunity to close any open descriptors
	// for files that have been removed.
	C.sd_journal_process(j)

	// Seek to the location of the last entry that we sent.
	if oldCursor != nil {
		// We know which entry was read last, so try to go to that
		// location.
		rc := C.sd_journal_seek_cursor(j, oldCursor)
		if rc != 0 {
			return oldCursor, false
		}
		// Go forward to the first unsent message.
		rc = C.sd_journal_next(j)
		if rc < 0 {
			return oldCursor, false
		}
		// We want to avoid sending a given entry twice (or more), so
		// attempt to advance to the first unread entry in the journal
		// so long as "this" one matches the last entry that we read.
		for C.sd_journal_test_cursor(j, oldCursor) > 0 {
			if C.sd_journal_next(j) <= 0 {
				return oldCursor, false
			}
		}
	}

	// Walk the journal from here forward until we run out of new entries.
	sent := uint64(0)
	eof := false
	for {
		// If we're not keeping up with journald writing to the journal, some of the
		// files between where we are and "now" may have been deleted since we started
		// walking the set of entries.  If that's happened, the inotify descriptor in
		// the journal handle will have pending deletion events after we've been reading
		// for a while.  Letting the journal library process them will close any that
		// are already deleted, so that we'll skip over them and allow space that would
		// have been reclaimed by deleting these files to actually be reclaimed.
		if sent > 0 && sent%1024 == 0 {
			if status := C.sd_journal_process(j); status < 0 {
				cerrstr := C.strerror(C.int(-status))
				errstr := C.GoString(cerrstr)
				fmtstr := "error %q while attempting to process journal events for container %q"
				logrus.Errorf(fmtstr, errstr, s.vars["CONTAINER_ID_FULL"])
				// Attempt to rewind the last-read cursor to the
				// entry that we last sent.
				if status = C.sd_journal_previous(j); status < 0 {
					cerrstr := C.strerror(C.int(-status))
					errstr := C.GoString(cerrstr)
					fmtstr := "error %q while attempting to rewind journal by 1 for container %q"
					logrus.Errorf(fmtstr, errstr, s.vars["CONTAINER_ID_FULL"])
				}
				break
			}
		}
		// If the output channel is full, stop here, so that we don't block indefinitely
		// waiting until we can output another message, which won't ever happen if the
		// client has already disconnected.
		if len(logWatcher.Msg) >= cap(logWatcher.Msg) {
			// Attempt to rewind the last-read cursor to the entry
			// that we last sent.
			if status := C.sd_journal_previous(j); status < 0 {
				cerrstr := C.strerror(C.int(-status))
				errstr := C.GoString(cerrstr)
				fmtstr := "error %q while attempting to rewind journal by 1 for container %q"
				logrus.Errorf(fmtstr, errstr, s.vars["CONTAINER_ID_FULL"])
			}
			break
		}
		// Read and send the current message, if there is one to read.
		i := C.get_message(j, &msg, &length, &partial)
		if i != -C.ENOENT && i != -C.EADDRNOTAVAIL {
			// Read the entry's timestamp.
			if C.sd_journal_get_realtime_usec(j, &stamp) != 0 {
				// Attempt to rewind the last-read
				// cursor to the entry that we last
				// sent.
				if status := C.sd_journal_previous(j); status < 0 {
					cerrstr := C.strerror(C.int(-status))
					errstr := C.GoString(cerrstr)
					fmtstr := "error %q while attempting to rewind journal by 1 for container %q"
					logrus.Errorf(fmtstr, errstr, s.vars["CONTAINER_ID_FULL"])
				}
				break
			}
			// Set up the time and text of the entry.
			timestamp := time.Unix(int64(stamp)/1000000, (int64(stamp)%1000000)*1000)
			line := C.GoBytes(unsafe.Pointer(msg), C.int(length))
			if partial == 0 {
				line = append(line, "\n"...)
			}
			// Recover the stream name by mapping
			// from the journal priority back to
			// the stream that we would have
			// assigned that value.
			source := ""
			if C.get_priority(j, &priority) != 0 {
				source = ""
			} else if priority == C.int(journal.PriErr) {
				source = "stderr"
			} else if priority == C.int(journal.PriInfo) {
				source = "stdout"
			}
			// Retrieve the values of any variables we're adding to the journal.
			attrs := make(map[string]string)
			C.sd_journal_restart_data(j)
			for C.get_attribute_field(j, &data, &length) > C.int(0) {
				kv := strings.SplitN(C.GoStringN(data, C.int(length)), "=", 2)
				attrs[kv[0]] = kv[1]
			}
			if len(attrs) == 0 {
				attrs = nil
			}
			// Send the log message.
			logWatcher.Msg <- &logger.Message{
				Line:      line,
				Source:    source,
				Timestamp: timestamp.In(time.UTC),
				Attrs:     attrs,
			}
		}
		// If we've hit the end of the journal, we're done (for now).
		sent++
		if C.sd_journal_next(j) <= 0 {
			eof = true
			break
		}
	}

	// If we didn't send any entries, just return the same cursor value.
	if oldCursor != nil && sent == 0 {
		return oldCursor, eof
	}
	// free(NULL) is safe
	C.free(unsafe.Pointer(oldCursor))
	// Take note of which entry we most recently sent.
	if C.sd_journal_get_cursor(j, &cursor) != 0 {
		// ensure that we won't be freeing an address that's invalid
		cursor = nil
	}
	return cursor, eof
}

func (s *journald) followJournal(logWatcher *logger.LogWatcher, config logger.ReadConfig, j *C.sd_journal, pfd [2]C.int, cursor *C.char) *C.char {
	s.readers.mu.Lock()
	s.readers.readers[logWatcher] = logWatcher
	s.readers.mu.Unlock()

	newCursor := make(chan *C.char)

	go func() {
		// Keep copying journal data out until we're notified to stop
		// or we hit an error.
		status := C.wait_for_data_cancelable(j, pfd[0])
		for status == 1 {
			cursor, _ = s.drainJournal(logWatcher, config, j, cursor)
			status = C.wait_for_data_cancelable(j, pfd[0])
		}
		if status < 0 {
			cerrstr := C.strerror(C.int(-status))
			errstr := C.GoString(cerrstr)
			fmtstr := "error %q while attempting to follow journal for container %q"
			logrus.Errorf(fmtstr, errstr, s.vars["CONTAINER_ID_FULL"])
		} else {
			// In the event that we were told to stop (logWatcher.WatchClose() below), it's possible
			// there's more data in the journal for this container that was written just as the container
			// exited. Try to drain the journal one more time to pick up any last-minute journal entries.
			// Note, this isn't fool-proof and there's no guarantee that we'll get all the trailing
			// entries, but this is better than nothing, as it does yield entries more often than not.
			cursor, _ = s.drainJournal(logWatcher, config, j, cursor)
		}
		// Clean up.
		C.close(pfd[0])
		s.readers.mu.Lock()
		delete(s.readers.readers, logWatcher)
		s.readers.mu.Unlock()
		close(logWatcher.Msg)
		newCursor <- cursor
	}()
	// Wait until we're told to stop.
	select {
	case <-logWatcher.WatchClose():
		// Notify the other goroutine that its work is done.
		C.close(pfd[1])
	}

	cursor = <-newCursor

	return cursor
}

func (s *journald) readLogs(logWatcher *logger.LogWatcher, config logger.ReadConfig) {
	var j *C.sd_journal
	var cmatch, cursor *C.char
	var stamp C.uint64_t
	var initiated C.uint64_t = 0xffffffffffffffff
	var sinceUnixMicro uint64
	var pipes [2]C.int
	var ts C.struct_timespec

	// Get the current time, so that we know when to stop in non-follow mode.
	if C.clock_gettime(C.CLOCK_REALTIME, &ts) == 0 {
		initiated = C.uint64_t(ts.tv_sec)*1000000000 + C.uint64_t(ts.tv_nsec)
	}
	// Get a handle to the journal.
	rc := C.sd_journal_open(&j, C.int(0))
	if rc != 0 {
		logWatcher.Err <- fmt.Errorf("error opening journal")
		close(logWatcher.Msg)
		return
	}
	// The journal library uses an inotify descriptor to notice when
	// journal files are removed, but it isn't allocated until our first
	// call to sd_journal_get_fd(), which means that it will not notice the
	// removal of any files that happens after we open the journal and
	// before the first time we try to read that descriptor.  Do it now,
	// even though we don't need its value just yet, to try to make that
	// window smaller.
	rc = C.sd_journal_get_fd(j)
	if rc < 0 {
		logWatcher.Err <- fmt.Errorf("error opening journal inotify descriptor")
		close(logWatcher.Msg)
		return
	}
	// If we end up following the log, we can set the journal context
	// pointer and the channel pointer to nil so that we won't close them
	// here, potentially while the goroutine that uses them is still
	// running.  Otherwise, close them when we return from this function.
	following := false
	defer func() {
		if !following {
			close(logWatcher.Msg)
		}
		C.sd_journal_close(j)
	}()
	// Remove limits on the size of data items that we'll retrieve.
	rc = C.sd_journal_set_data_threshold(j, C.size_t(0))
	if rc != 0 {
		logWatcher.Err <- fmt.Errorf("error setting journal data threshold")
		return
	}
	// Add a match to have the library do the searching for us.
	cmatch = C.CString("CONTAINER_ID_FULL=" + s.vars["CONTAINER_ID_FULL"])
	defer C.free(unsafe.Pointer(cmatch))
	rc = C.sd_journal_add_match(j, unsafe.Pointer(cmatch), C.strlen(cmatch))
	if rc != 0 {
		logWatcher.Err <- fmt.Errorf("error setting journal match")
		return
	}
	// If we have a cutoff time, convert it to Unix time once.
	if !config.Since.IsZero() {
		nano := config.Since.UnixNano()
		sinceUnixMicro = uint64(nano / 1000)
	}
	if config.Tail > 0 {
		lines := config.Tail
		// Start at the end of the journal.
		if C.sd_journal_seek_tail(j) < 0 {
			logWatcher.Err <- fmt.Errorf("error seeking to end of journal")
			return
		}
		if C.sd_journal_previous(j) < 0 {
			logWatcher.Err <- fmt.Errorf("error backtracking to previous journal entry")
			return
		}
		// Walk backward.
		for lines > 0 {
			// Stop if the entry time is before our cutoff.
			// We'll need the entry time if it isn't, so go
			// ahead and parse it now.
			if C.sd_journal_get_realtime_usec(j, &stamp) != 0 {
				break
			} else {
				// Compare the timestamp on the entry
				// to our threshold value.
				if sinceUnixMicro != 0 && sinceUnixMicro > uint64(stamp) {
					break
				}
			}
			lines--
			// If we're at the start of the journal, or
			// don't need to back up past any more entries,
			// stop.
			if lines == 0 || C.sd_journal_previous(j) <= 0 {
				break
			}
		}
	} else {
		// Start at the beginning of the journal.
		if C.sd_journal_seek_head(j) < 0 {
			logWatcher.Err <- fmt.Errorf("error seeking to start of journal")
			return
		}
		// If we have a cutoff date, fast-forward to it.
		if sinceUnixMicro != 0 && C.sd_journal_seek_realtime_usec(j, C.uint64_t(sinceUnixMicro)) != 0 {
			logWatcher.Err <- fmt.Errorf("error seeking to start time in journal")
			return
		}
		if C.sd_journal_next(j) < 0 {
			logWatcher.Err <- fmt.Errorf("error skipping to next journal entry")
			return
		}
	}
	cursor, eof := s.drainJournal(logWatcher, config, j, nil)
	if config.Follow {
		// Allocate a descriptor for following the journal, if we'll
		// need one.  Do it here so that we can report if it fails.
		if fd := C.sd_journal_get_fd(j); fd < C.int(0) {
			logWatcher.Err <- fmt.Errorf("error opening journald follow descriptor: %q", C.GoString(C.strerror(-fd)))
		} else {
			// Create a pipe that we can poll at the same time as
			// the journald descriptor.
			if C.pipe(&pipes[0]) == C.int(-1) {
				logWatcher.Err <- fmt.Errorf("error opening journald close notification pipe")
			} else {
				cursor = s.followJournal(logWatcher, config, j, pipes, cursor)
				// Let followJournal handle freeing the journal context
				// object and closing the channel.
				following = true
			}
		}
	} else {
		// In case we stopped reading because the output channel was
		// temporarily full, keep going until we cross the point where
		// the timestamps on entries are later than when we started
		// reading the log, to avoid trying to keep going until we
		// hit the end of the journal when we just can't keep up.
		duration := 10 * time.Millisecond
		timer := time.NewTimer(duration)
	drainCatchup:
		for !eof && stamp < initiated {
			timer.Stop()
			cursor, eof = s.drainJournal(logWatcher, config, j, cursor)
			if C.sd_journal_get_realtime_usec(j, &stamp) != 0 {
				break drainCatchup
			}
			timer.Reset(duration)
			select {
			case <-logWatcher.WatchClose():
				break drainCatchup
			case <-timer.C:
			}
		}
		timer.Stop()
	}

	C.free(unsafe.Pointer(cursor))
	return
}

func (s *journald) ReadLogs(config logger.ReadConfig) *logger.LogWatcher {
	logWatcher := logger.NewLogWatcher()
	go s.readLogs(logWatcher, config)
	return logWatcher
}
