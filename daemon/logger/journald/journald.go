// +build linux

// Package journald provides the log driver for forwarding server logs
// to endpoints that receive the systemd format.
package journald

import (
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/journal"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/loggerutils"
	"github.com/docker/docker/pkg/reexec"
	"golang.org/x/sys/unix"
)

const name = "journald"
const handler = "journal-logger"

type journald struct {
	// for reading
	vars    map[string]string // additional variables and values to send to the journal along with the log message
	readers readerList
	// for writing
	writing sync.Mutex
	cmd     *exec.Cmd
	pipe    io.WriteCloser
	encoder *gob.Encoder
}

type readerList struct {
	mu      sync.Mutex
	readers map[*logger.LogWatcher]*logger.LogWatcher
}

// MessageWithVars describes the packet format that we use when forwarding log
// messages from the daemon to a helper process.
type MessageWithVars struct {
	logger.Message
	Vars map[string]string
}

func init() {
	if err := logger.RegisterLogDriver(name, New); err != nil {
		logrus.Fatal(err)
	}
	if err := logger.RegisterLogOptValidator(name, validateLogOpt); err != nil {
		logrus.Fatal(err)
	}
	gob.Register(MessageWithVars{})
	reexec.Register(handler, journalLoggerMain)
}

// sanitizeKeyMode returns the sanitized string so that it could be used in journald.
// In journald log, there are special requirements for fields.
// Fields must be composed of uppercase letters, numbers, and underscores, but must
// not start with an underscore.
func sanitizeKeyMod(s string) string {
	n := ""
	for _, v := range s {
		if 'a' <= v && v <= 'z' {
			v = unicode.ToUpper(v)
		} else if ('Z' < v || v < 'A') && ('9' < v || v < '0') {
			v = '_'
		}
		// If (n == "" && v == '_'), then we will skip as this is the beginning with '_'
		if !(n == "" && v == '_') {
			n += string(v)
		}
	}
	return n
}

// New creates a journald logger using the configuration passed in on
// the context.
func New(ctx logger.Context) (logger.Logger, error) {
	if !journal.Enabled() {
		return nil, fmt.Errorf("journald is not enabled on this host")
	}

	// parse the log tag
	tag, err := loggerutils.ParseLogTag(ctx, loggerutils.DefaultTemplate)
	if err != nil {
		return nil, err
	}
	// build the set of values which we'll send to the journal every time
	vars := map[string]string{
		"CONTAINER_ID":      ctx.ID(),
		"CONTAINER_ID_FULL": ctx.FullID(),
		"CONTAINER_NAME":    ctx.Name(),
		"CONTAINER_TAG":     tag,
	}
	extraAttrs := ctx.ExtraAttributes(sanitizeKeyMod)
	for k, v := range extraAttrs {
		vars[k] = v
	}
	// start the helper
	cgroupSpec, err := ctx.CGroup()
	if err != nil {
		return nil, err
	}
	cmd := reexec.Command(handler, cgroupSpec)
	cmd.Dir = "/"
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("error opening pipe to logging helper: %v", err)
	}
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("error starting logging helper: %v", err)
	}
	encoder := gob.NewEncoder(pipe)
	// gather up everything we need to hand back
	j := &journald{
		vars:    vars,
		readers: readerList{readers: make(map[*logger.LogWatcher]*logger.LogWatcher)},
		cmd:     cmd,
		pipe:    pipe,
		encoder: encoder,
	}
	return j, nil
}

// We don't actually accept any options, but we have to supply a callback for
// the factory to pass the (probably empty) configuration map to.
func validateLogOpt(cfg map[string]string) error {
	for key := range cfg {
		switch key {
		case "labels":
		case "env":
		case "tag":
		default:
			return fmt.Errorf("unknown log opt '%s' for journald log driver", key)
		}
	}
	return nil
}

func (s *journald) Log(msg *logger.Message) error {
	// build the message struct for the helper, and send it on down
	message := MessageWithVars{
		Message: *msg,
		Vars:    s.vars,
	}
	s.writing.Lock()
	defer s.writing.Unlock()
	return s.encoder.Encode(&message)
}

func (s *journald) Name() string {
	return name
}

func (s *journald) closeWriter() {
	s.pipe.Close()
	if err := s.cmd.Wait(); err != nil {
		eerr, ok := err.(*exec.ExitError)
		if !ok {
			logrus.Errorf("error waiting on log handler: %v", err)
			return
		}
		status, ok := eerr.Sys().(syscall.WaitStatus)
		if !ok {
			logrus.Errorf("error waiting on log handler: %v", err)
			return
		}
		if !status.Signaled() || (status.Signal() != syscall.SIGTERM && status.Signal() != syscall.SIGKILL) {
			logrus.Errorf("error waiting on log handler: %v", err)
			return
		}
	}
}

func loggerLog(f string, args ...interface{}) {
	s := fmt.Sprintf(f, args...)
	journal.Send(s, journal.PriInfo, nil)
	fmt.Fprintln(os.Stderr, s)
}

func joinScope(scope string) error {
	// This is... not ideal.  But if we're here, we're just going to have
	// to assume that we know how to compute the same path that runc is
	// going to use, based on a value of the form "parent:docker:ID", where
	// the "docker" is literal.
	parts := strings.Split(scope, ":")
	fs, err := os.Open("/sys/fs/cgroup")
	if err != nil {
		return err
	}
	defer fs.Close()
	mountPoint := fs.Name()
	controllers, err := fs.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, controller := range controllers {
		scopeDir := filepath.Join(mountPoint, controller, parts[0], parts[1]+"-"+parts[2]+".scope")
		procsFile := filepath.Join(scopeDir, "cgroup.procs")
		f, err := os.OpenFile(procsFile, os.O_WRONLY, 0644)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		defer f.Close()
		fmt.Fprintln(f, unix.Getpid())
	}
	return nil
}

func journalLoggerMain() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 0 {
		loggerLog("should be invoked with the name of the container's scope")
		return
	}
	joined := false
	decoder := gob.NewDecoder(os.Stdin)
	for {
		var msg MessageWithVars
		// wait for the next chunk of data to log
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			loggerLog("error decoding message: %v", err)
			continue
		}
		// if we haven't joined the container's scope yet, do that now
		if !joined {
			if err := joinScope(args[0]); err != nil {
				loggerLog("error joining scope %q: %v", args[0], err)
			}
			joined = true
		}
		msg.Vars["CONTAINER_SOURCE"] = msg.Source
		// add a note if this message is a partial message
		if msg.Partial {
			msg.Vars["CONTAINER_PARTIAL_MESSAGE"] = "true"
		}
		if msg.Source == "stderr" {
			journal.Send(string(msg.Line), journal.PriErr, msg.Vars)
			continue
		}
		journal.Send(string(msg.Line), journal.PriInfo, msg.Vars)
	}
}
