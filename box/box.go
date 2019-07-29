// Userland Cloud CLI
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

package box

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cypherpunkarmory/ulacli/backoff"
	"github.com/fatih/color"
	"github.com/shiena/ansicolor"
	log "github.com/sirupsen/logrus"
	"github.com/tj/go-spin"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type boxStatus struct {
	state string
}

const (
	boxError    string = "error"
	boxDone     string = "done"
	boxStarting string = "starting"
)

//StartBox Main box function. Handles connections and forwarding
func StartBox(boxConfig *Config, wg *sync.WaitGroup, semaphore *Semaphore) {
	defer cleanup(boxConfig)

	if wg != nil {
		defer wg.Done()
	}
	client, err := createBox(boxConfig, semaphore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		return
	}

	defer client.Close()

	// This catches CTRL C and closes the ssh
	startCloseChannel := make(chan os.Signal)
	signal.Notify(startCloseChannel,
		// https://www.gnu.org/software/libc/manual/html_node/Termination-Signals.html
		syscall.SIGTERM, // "the normal way to politely ask a program to terminate"
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGQUIT, // Ctrl-\
		syscall.SIGHUP,  // "terminal is disconnected"
	)
	session, err := client.NewSession()
	if err != nil {
		panic("Failed to create session: " + err.Error())
	}
	defer session.Close()

	// Set IO
	session.Stdout = ansicolor.NewAnsiColorWriter(os.Stdout)
	session.Stderr = ansicolor.NewAnsiColorWriter(os.Stderr)
	session.Stdin = os.Stdin

	// Set up terminal modes
	// https://net-ssh.github.io/net-ssh/classes/Net/SSH/Connection/Term.html
	// https://www.ietf.org/rfc/rfc4254.txt
	// https://godoc.org/golang.org/x/crypto/ssh
	// THIS IS THE TITLE
	// https://pythonhosted.org/ANSIColors-balises/ANSIColors.html
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	fileDescriptor := int(os.Stdin.Fd())

	if terminal.IsTerminal(fileDescriptor) {
		originalState, err := terminal.MakeRaw(fileDescriptor)
		if err != nil {
			log.Fatalf("Unable to put the terminal connected to a file descriptor into raw mode: %v", err)
		}
		defer terminal.Restore(fileDescriptor, originalState)

		termWidth, termHeight, err := terminal.GetSize(fileDescriptor)
		if err != nil {
			log.Fatalf("Unable to get the dimensions for the terminal: %v", err)
		}

		err = session.RequestPty("xterm-256color", termHeight, termWidth, modes)
		if err != nil {
			log.Fatalf("Unable to request pty for the session: %v", err)
		}
	}

	// Start remote shell
	if err := session.Shell(); err != nil {
		log.Fatalf("failed to start shell: %s", err)
	}

	go func() {
		<-startCloseChannel
		if semaphore.CanRun() {
			cleanup(boxConfig)
			os.Exit(0)
		}
	}()

	// Accepting commands
	session.Wait()
}

func createBox(boxConfig *Config, semaphore *Semaphore) (*ssh.Client, error) {
	boxCreating := boxStatus{boxStarting}
	createCloseChannel := make(chan os.Signal)
	signal.Notify(createCloseChannel,
		// https://www.gnu.org/software/libc/manual/html_node/Termination-Signals.html
		syscall.SIGTERM, // "the normal way to politely ask a program to terminate"
		syscall.SIGINT,  // Ctrl+C
		syscall.SIGQUIT, // Ctrl-\
		syscall.SIGHUP,  // "terminal is disconnected"
	)
	defer signal.Stop(createCloseChannel)
	go func() {
		<-createCloseChannel
		log.Debugf("Closing box")
		boxCreating.state = boxError
		for !semaphore.CanRun() {

		}
		defer semaphore.Done()
		cleanup(boxConfig)
		os.Exit(0)
	}()
	lvl, err := log.ParseLevel(boxConfig.LogLevel)
	if err != nil {
		log.Errorf("\nLog level %s is not a valid level.", boxConfig.LogLevel)
	}

	log.SetLevel(lvl)
	log.Debugf("Debug Logging activated")

	var client ssh.Client

	sshPort := boxConfig.Box.SSHPort

	var jumpServerEndpoint = Endpoint{
		Host: boxConfig.ConnectionEndpoint.Hostname(),
		Port: boxConfig.ConnectionEndpoint.Port(),
	}

	// remote SSH server
	var serverEndpoint = Endpoint{
		Host: boxConfig.Box.IPAddress,
		Port: sshPort,
	}

	privateKey, err := readPrivateKeyFile(boxConfig.PrivateKeyPath)
	if err != nil {
		return &client, err
	}

	hostKeyCallBack := dnsHostKeyCallback
	if boxConfig.ConnectionEndpoint.Hostname() != "api.userland.tech" {
		log.Debug("Ignoring hostkey for connection")
		hostKeyCallBack = ssh.InsecureIgnoreHostKey()
	}

	sshJumpConfig := &ssh.ClientConfig{
		User: "userland",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		HostKeyCallback: hostKeyCallBack,
		Timeout:         0,
	}

	log.Debugf("Dial into Jump Server %s", jumpServerEndpoint.String())
	jumpConn, err := ssh.Dial("tcp", jumpServerEndpoint.String(), sshJumpConfig)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error contacting the UserLAnd server.")
		log.Debugf("%s", err)
		return &client, err
	}

	boxStartingSpinner(semaphore, &boxCreating)
	exponentialBackoff := backoff.NewExponentialBackOff()

	// Connect to SSH remote server using serverEndpoint
	var serverConn net.Conn
	for {
		serverConn, err = jumpConn.Dial("tcp", serverEndpoint.String())
		log.Debugf("Dial into SSHD Container %s", serverEndpoint.String())
		if err == nil {
			boxCreating.state = boxDone
			break
		}
		wait := exponentialBackoff.NextBackOff()
		log.Debugf("Backoff Tick %s", wait.String())
		time.Sleep(wait)
	}

	sshBoxConfig := &ssh.ClientConfig{
		User: "userland",
		Auth: []ssh.AuthMethod{
			privateKey,
		},
		//TODO: Maybe fix this. Will be rotating so dont know if possible
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         0,
	}
	ncc, chans, reqs, err := ssh.NewClientConn(serverConn, serverEndpoint.String(), sshBoxConfig)
	if err != nil {
		return &client, err
	}
	log.Debugf("SSH Connection Established via Jump %s -> %s", jumpServerEndpoint.String(), serverEndpoint.String())

	sClient := ssh.NewClient(ncc, chans, reqs)
	// Listen on remote server port
	return sClient, nil
}

func boxStartingSpinner(lock *Semaphore, boxStatus *boxStatus) {
	go func() {
		if !lock.CanRun() {
			return
		}
		defer lock.Done()
		s := spin.New()
		for boxStatus.state == boxStarting {
			fmt.Printf("\rStarting box %s ", s.Next())
			time.Sleep(100 * time.Millisecond)
		}
		if boxStatus.state == boxDone {
			fmt.Printf("\rStarting box ")
			d := color.New(color.FgGreen, color.Bold)
			d.Printf("✔\n")
		}
	}()
}
func cleanup(config *Config) {
	fmt.Println("\nClosing box")
	config.RestAPI.SetRefreshToken(config.RestAPI.RefreshToken)
	errSession := config.RestAPI.StartSession(config.RestAPI.RefreshToken)
	config.RestAPI.SetAPIKey(config.RestAPI.APIKey)
	errDelete := config.RestAPI.DeleteBoxAPI(config.Box.ID)
	if errSession != nil || errDelete != nil {
		fmt.Fprintf(os.Stderr,
			"We had some trouble deleting your box\n")
	}
}
