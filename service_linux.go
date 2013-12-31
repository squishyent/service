package service

import (
	"fmt"
	"github.com/squishyent/osext"
	"log/syslog"
	"os"
	"os/exec"
	"os/signal"
	"text/template"
)

func newService(name, displayName, description, exePath string) (s *linuxService, err error) {
	if _, err := os.Stat("/etc/redhat-release"); err == nil {
		s, err = newChkconfigService(name, displayName, description, exePath)
	} else {
		s, err = newUpstartService(name, displayName, description, exePath)
	}
	if err != nil {
		return nil, err
	}

	s.logger, err = syslog.New(syslog.LOG_INFO, name)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func newUpstartService(name, displayName, description, exePath string) (s *linuxService, err error) {
	return &linuxService{
		name:           name,
		displayName:    displayName,
		description:    description,
		exePath:        exePath,
		scriptTemplate: upstartScript,
		scriptPath:     "/etc/init/" + name + ".conf",
		scriptPerms:    0644,
		startCmd:       []string{"start", name},
		stopCmd:        []string{"stop", name},
	}, nil
}

func newChkconfigService(name, displayName, description, exePath string) (s *linuxService, err error) {
	return &linuxService{
		name:           name,
		displayName:    displayName,
		description:    description,
		exePath:        exePath,
		scriptPath:     "/etc/init.d/" + name,
		scriptTemplate: initScript,
		scriptPerms:    0755,
		installCmd:     []string{"chkconfig", "--add", name},
		removeCmd:      []string{"chkconfig", "--del", name},
		startCmd:       []string{"service", name, "start"},
		stopCmd:        []string{"service", name, "stop"},
	}, nil
}

type linuxService struct {
	name, displayName, description, exePath string
	logger                                  *syslog.Writer

	scriptPath, scriptTemplate               string
	scriptPerms                              os.FileMode
	installCmd, removeCmd, startCmd, stopCmd []string
}

func (s *linuxService) Install() error {
	_, err := os.Stat(s.scriptPath)
	if err == nil {
		return fmt.Errorf("Init already exists: %s", s.scriptPath)
	}

	f, err := os.Create(s.scriptPath)
	if err != nil {
		return err
	}
	defer f.Close()

	path := s.exePath
	if path == "" {
		path, err = osext.Executable()
		if err != nil {
			return err
		}
	}

	var to = &struct {
		Name        string
		Display     string
		Description string
		Path        string
	}{
		s.name,
		s.displayName,
		s.description,
		path,
	}

	t := template.Must(template.New("serverScript").Parse(s.scriptTemplate))
	err = t.Execute(f, to)

	if err != nil {
		return err
	}

	if err := os.Chmod(s.scriptPath, s.scriptPerms); err != nil {
		return err
	}

	if len(s.installCmd) > 0 {
		cmd := exec.Command(s.installCmd[0], s.installCmd[1:]...)
		return cmd.Run()
	}

	return nil
}

func (s *linuxService) Remove() error {
	if len(s.removeCmd) > 0 {
		cmd := exec.Command(s.removeCmd[0], s.removeCmd[1:]...)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	return os.Remove(s.scriptPath)
}

func (s *linuxService) Run(onStart, onStop func() error) error {
	var err error

	err = onStart()
	if err != nil {
		return err
	}

	var sigChan = make(chan os.Signal, 3)

	signal.Notify(sigChan, os.Interrupt, os.Kill)

	<-sigChan

	return onStop()
}

func (s *linuxService) Start() error {
	cmd := exec.Command(s.startCmd[0], s.startCmd[1:]...)
	return cmd.Run()
}

func (s *linuxService) Stop() error {
	cmd := exec.Command(s.stopCmd[0], s.stopCmd[1:]...)
	return cmd.Run()
}

func (s *linuxService) Error(format string, a ...interface{}) error {
	return s.logger.Err(fmt.Sprintf(format, a...))
}
func (s *linuxService) Warning(format string, a ...interface{}) error {
	return s.logger.Warning(fmt.Sprintf(format, a...))
}
func (s *linuxService) Info(format string, a ...interface{}) error {
	return s.logger.Info(fmt.Sprintf(format, a...))
}

// A script for upstart
var upstartScript = `# {{.Description}}

description     "{{.Display}}"

start on filesystem or runlevel [2345]
stop on runlevel [!2345]

#setuid username

kill signal INT

respawn
respawn limit 10 5
umask 022

console none

pre-start script
    test -x {{.Path}} || { stop; exit 0; }
end script

# Start
exec {{.Path}}
`

// A script for init.d
var initScript = `#!/bin/bash
#
# {{.Name}} {{.Display}}
#
# chkconfig: 2345 99 01
# description: {{.Description}}
# processname: {{.Name}}

source /etc/rc.d/init.d/functions

RETVAL=0

usage()
{
	echo $"Usage: $0 {start|stop|restart}" 1>&2
	RETVAL=2
}

restart()
{
	stop
	start
}

start() {
  echo -n $"Starting {{.Name}} ({{.Path}}): "
	daemon {{.Path}}
	RETVAL=$?
	echo
}

stop() {
	echo -n $"Shutting down {{.Name}} ({{.Path}}): "
	killproc {{.Path}}
	RETVAL=$?
	[ $RETVAL -eq 0 ] && success || failure
	echo
	return $RETVAL
}

case "$1" in
    stop) stop ;;
    start|restart|reload|force-reload) restart ;;
    *) usage ;;
esac

exit $RETVAL
`
