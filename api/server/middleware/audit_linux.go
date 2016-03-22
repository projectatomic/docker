package middleware

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/user"
	"path"
	"reflect"
	"strconv"
	"syscall"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/container"
	"github.com/docker/docker/daemon"
	"github.com/docker/docker/pkg/audit"
	"github.com/docker/engine-api/types/versions/v1p20"
	"golang.org/x/net/context"
)

// WrapHandler returns a new handler function wrapping the previous one in the request chain.
func (a AuditMiddleware) WrapHandler(handler func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error) func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
		logAction(w, r, a.d)
		return handler(ctx, w, r, vars)
	}
}

//Gets the file descriptor
func getFdFromWriter(w http.ResponseWriter) int {
	//We must use introspection to pull the
	//connection from the ResponseWriter object
	//This is because the connection object is not exported by the writer.
	writerVal := reflect.Indirect(reflect.ValueOf(w))
	if writerVal.Kind() != reflect.Struct {
		logrus.Warnf("ResponseWriter is not a struct but %s", writerVal.Kind())
		return -1
	}
	httpconn := writerVal.FieldByName("conn")
	if !httpconn.IsValid() {
		// probably writerVal contains "rw" which is the ResponseWriter
		rwPtr := writerVal.FieldByName("rw")
		if !rwPtr.IsValid() {
			logrus.Warn("ResponseWriter does not contain a field named conn nor rw")
			return -1
		}
		writerVal = reflect.Indirect(rwPtr.Elem())
		if writerVal.Kind() != reflect.Struct {
			logrus.Warnf("ResponseWriter is not a struct but %s", writerVal.Kind())
			return -1
		}
	}
	//Get the underlying http connection
	httpconnVal := writerVal.FieldByName("conn").Elem()
	if httpconnVal.Kind() != reflect.Struct {
		logrus.Warnf("conn is not an interface to a struct but %s", httpconnVal.Kind())
		return -1
	}
	//Get the underlying tcp connection
	rwcPtr := httpconnVal.FieldByName("rwc").Elem()
	rwc := rwcPtr.Elem()
	if rwc.Kind() != reflect.Struct {
		logrus.Warnf("conn is not an interface to a struct but %s", rwc.Kind())
		return -1
	}

	var c reflect.Value
	if rwc.Field(0).Kind() != reflect.Struct {
		// this is the case of the unix socket with go1.6 compatibility fix!
		cPtr := rwc.Field(0).Elem()
		c = cPtr.Elem()
	} else {
		// this is the normal tcp case
		c = rwc.FieldByName("conn")
	}
	netfd := c.FieldByName("fd").Elem()
	//Grab sysfd
	if netfd.Kind() != reflect.Struct {
		logrus.Warnf("fd is not a struct but %s", netfd.Kind())
		return -1
	}
	sysfd := netfd.FieldByName("sysfd")
	//Finally, we have the fd
	return int(sysfd.Int())
}

//Gets the ucred given an http response writer
func getUcred(fd int) (*syscall.Ucred, error) {
	return syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
}

//Gets the client's loginuid
func getLoginUID(ucred *syscall.Ucred, fd int) (int64, error) {
	if _, err := syscall.Getpeername(fd); err != nil {
		logrus.Errorf("Socket appears to have closed: %v", err)
		return -1, err
	}
	loginuid, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/loginuid", ucred.Pid))
	if err != nil {
		logrus.Errorf("Error reading loginuid: %v", err)
		return -1, err
	}
	loginuidInt, err := strconv.ParseInt(string(loginuid), 10, 0)
	if err != nil {
		logrus.Errorf("Failed to convert loginuid to int: %v", err)
		return -1, err
	}
	return loginuidInt, nil
}

//Given a loginUID, retrieves the current username
func getpwuid(loginUID uint32) (string, error) {
	uid := strconv.FormatUint(uint64(loginUID), 10)
	pwd, err := user.LookupId(uid)
	if err != nil {
		logrus.Errorf("Failed to get pwuid struct for %d: %v", loginUID, err)
		return "", err
	}
	if pwd == nil {
		return "", user.UnknownUserIdError(loginUID)
	}
	name := pwd.Username
	return name, nil
}

//Retrieves the container and "action" (start, stop, kill, etc) from the http request
func parseRequest(r *http.Request, d *daemon.Daemon) (string, *container.Container) {
	var (
		containerID string
		action      string
	)
	requrl := r.RequestURI
	parsedurl, err := url.Parse(requrl)
	if err != nil {
		return "?", nil
	}

	switch r.Method {
	//Delete requests do not explicitly state the action, so we check the HTTP method instead
	case "DELETE":
		action = "remove"
		containerID = path.Base(parsedurl.Path)
	default:
		action = path.Base(parsedurl.Path)
		containerID = path.Base(path.Dir(parsedurl.Path))
	}

	if d != nil {
		c, err := d.GetContainer(containerID)
		if err == nil {
			return action, c
		}
	}
	return action, nil
}

//Traverses the config struct and grabs non-standard values for logging
func parseConfig(config interface{}) string {
	configReflect := reflect.Indirect(reflect.ValueOf(config))
	var result bytes.Buffer
	for index := 0; index < configReflect.NumField(); index++ {
		val := reflect.Indirect(configReflect.Field(index))
		//Get the zero value of the struct's field
		if val.IsValid() {
			zeroVal := reflect.Zero(val.Type()).Interface()
			//If the configuration value is not a zero value, then we store it
			//We use deep equal here because some types cannot be compared with the standard equality operators
			if val.Kind() == reflect.Bool || !reflect.DeepEqual(zeroVal, val.Interface()) {
				fieldName := configReflect.Type().Field(index).Name
				if result.Len() > 0 {
					result.WriteString(", ")
				}
				fmt.Fprintf(&result, "%s=%+v", fieldName, val.Interface())
			}
		}
	}
	return result.String()
}

//Constructs a partial log message containing the container's configuration settings
func generateContainerConfigMsg(c *container.Container, j *v1p20.ContainerJSON) string {
	if c != nil && j != nil {
		configStripped := parseConfig(*c.Config)
		hostConfigStripped := parseConfig(*j.HostConfig)
		return fmt.Sprintf("Config={%v}, HostConfig={%v}", configStripped, hostConfigStripped)
	}
	return ""
}

func logAction(w http.ResponseWriter, r *http.Request, d *daemon.Daemon) error {
	var (
		message  string
		username string
		loginuid int64 = -1
	)

	action, c := parseRequest(r, d)

	switch action {
	case "start":
		if d != nil && c != nil {
			inspect, err := d.ContainerInspect(c.ID, false, "1.20")
			if err == nil {
				message = ", " + generateContainerConfigMsg(c, inspect.(*v1p20.ContainerJSON))
			}
		}
		fallthrough
	default:
		//Get user credentials
		fd := getFdFromWriter(w)
		if fd == -1 {
			message = "LoginUID unknown, PID unknown" + message
			break
		}
		server, err := syscall.Getsockname(fd)
		if err != nil {
			logrus.Errorf("Unable to read peer creds and server socket address: %v", err)
			message = "LoginUID unknown, PID unknown" + message
			break
		}
		if _, isUnix := server.(*syscall.SockaddrUnix); !isUnix {
			logrus.Debug("Unable to read peer creds: server socket is not a Unix socket")
			message = "LoginUID unknown, PID unknown" + message
			break
		}
		ucred, err := getUcred(fd)
		if err != nil {
			logrus.Errorf("Unable to read peer creds: %v", err)
			message = "LoginUID unknown, PID unknown" + message
			break
		}
		message = fmt.Sprintf("PID=%v", ucred.Pid) + message

		//Get user loginuid
		loginuid, err = getLoginUID(ucred, fd)
		if err != nil {
			break
		}
		message = fmt.Sprintf("LoginUID=%v, %s", loginuid, message)
		if loginuid < 0 || loginuid >= 0xffffffff { // -1 means no login user
			//No login UID is set, so no point in looking up a name
			break
		}

		//Get username
		username, err = getpwuid(uint32(loginuid))
		if err != nil {
			break
		}

		message = fmt.Sprintf("Username=%v, %s", username, message)
	}

	//Log the container ID being affected if it exists
	if c != nil {
		message = fmt.Sprintf("ID=%v, %s", c.ID, message)
	}
	message = fmt.Sprintf("{Action=%v, %s}", action, message)
	// Log info messages at Debug Level
	// Log messages that change state at Info level
	switch action {
	case "history", "events", "stats", "search", "json", "version", "images", "info":
		logrus.Debug(message)
	default:
		logrus.Info(message)
		logAuditlog(c, action, username, loginuid, true)
	}
	return nil
}

//Logs an API event to the audit log
func logAuditlog(c *container.Container, action string, username string, loginuid int64, success bool) {
	virt := audit.AuditVirtControl
	vm := "?"
	vmPid := "?"
	exe := "?"
	hostname := "?"
	user := "?"
	auid := "?"

	if c != nil {
		vm = c.Config.Image
		vmPid = fmt.Sprint(c.State.Pid)
		exe = c.Path
		hostname = c.Config.Hostname
	}

	if username != "" {
		user = username
	}

	if loginuid != -1 {
		auid = fmt.Sprint(loginuid)
	}

	vars := map[string]string{
		"op":       action,
		"reason":   "api",
		"vm":       vm,
		"vm-pid":   vmPid,
		"user":     user,
		"auid":     auid,
		"exe":      exe,
		"hostname": hostname,
	}

	//Encoding is a function of libaudit that ensures
	//that the audit values contain only approved characters.
	for key, value := range vars {
		if audit.ValueNeedsEncoding(value) {
			vars[key] = audit.EncodeNVString(key, value)
		}
	}
	message := audit.FormatVars(vars)
	audit.LogUserEvent(virt, message, success)
}
