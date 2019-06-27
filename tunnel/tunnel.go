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

package tunnel

import (
	"bufio"
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
)

type tunnelStatus struct {
	state string
}

const (
	tunnelError    string = "error"
	tunnelDone     string = "done"
	tunnelStarting string = "starting"
)

//StartTunnel Main tunneling function. Handles connections and forwarding
func StartTunnel(tunnelConfig *Config, wg *sync.WaitGroup, semaphore *Semaphore) {
	defer cleanup(tunnelConfig)

	if wg != nil {
		defer wg.Done()
	}
	client, err := createBox(tunnelConfig, semaphore)
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
	in, _ := session.StdinPipe()

	// Set up terminal modes
	// https://net-ssh.github.io/net-ssh/classes/Net/SSH/Connection/Term.html
	// https://www.ietf.org/rfc/rfc4254.txt
	// https://godoc.org/golang.org/x/crypto/ssh
	// THIS IS THE TITLE
	// https://pythonhosted.org/ANSIColors-balises/ANSIColors.html
	modes := ssh.TerminalModes{
		ssh.ECHO:  0, // Disable echoing
		ssh.IGNCR: 1, // Ignore CR on input.
	}

	// Request pseudo terminal
	//if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
	//if err := session.RequestPty("xterm-256color", 80, 40, modes); err != nil {
	if err := session.RequestPty("vt100", 80, 40, modes); err != nil {
		//if err := session.RequestPty("vt220", 80, 40, modes); err != nil {
		log.Fatalf("request for pseudo terminal failed: %s", err)
	}

	// Start remote shell
	if err := session.Shell(); err != nil {
		log.Fatalf("failed to start shell: %s", err)
	}

	go func() {
		<-startCloseChannel
		if semaphore.CanRun() {
			cleanup(tunnelConfig)
			os.Exit(0)
		}
	}()

	// Accepting commands
	for {
		reader := bufio.NewReader(os.Stdin)
		str, _ := reader.ReadString('\n')
		fmt.Fprint(in, str)
	}

}

func createBox(tunnelConfig *Config, semaphore *Semaphore) (*ssh.Client, error) {
	tunnelCreating := tunnelStatus{tunnelStarting}
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
		log.Debugf("Closing tunnel")
		tunnelCreating.state = tunnelError
		for !semaphore.CanRun() {

		}
		defer semaphore.Done()
		cleanup(tunnelConfig)
		os.Exit(0)
	}()
	lvl, err := log.ParseLevel(tunnelConfig.LogLevel)
	if err != nil {
		log.Errorf("\nLog level %s is not a valid level.", tunnelConfig.LogLevel)
	}

	log.SetLevel(lvl)
	log.Debugf("Debug Logging activated")

	var client ssh.Client

	sshPort := tunnelConfig.TunnelEndpoint.SSHPort

	var jumpServerEndpoint = Endpoint{
		Host: tunnelConfig.ConnectionEndpoint.Hostname(),
		Port: tunnelConfig.ConnectionEndpoint.Port(),
	}

	// remote SSH server
	var serverEndpoint = Endpoint{
		Host: tunnelConfig.TunnelEndpoint.IPAddress,
		Port: sshPort,
	}

	privateKey, err := readPrivateKeyFile(tunnelConfig.PrivateKeyPath)
	if err != nil {
		return &client, err
	}

	hostKeyCallBack := dnsHostKeyCallback
	if tunnelConfig.ConnectionEndpoint.Hostname() != "api.userland.io" {
		fmt.Println("Ignoring hostkey")
		hostKeyCallBack = ssh.InsecureIgnoreHostKey()
	}

	sshJumpConfig := &ssh.ClientConfig{
		User: "punch",
		Auth: []ssh.AuthMethod{
			privateKey,
		},
		HostKeyCallback: hostKeyCallBack,
		Timeout:         0,
	}

	log.Debugf("Dial into Jump Server %s", jumpServerEndpoint.String())
	jumpConn, err := ssh.Dial("tcp", jumpServerEndpoint.String(), sshJumpConfig)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error contacting the holepunch Server.")
		log.Debugf("%s", err)
		return &client, err
	}

	tunnelStartingSpinner(semaphore, &tunnelCreating)
	exponentialBackoff := backoff.NewExponentialBackOff()

	// Connect to SSH remote server using serverEndpoint
	var serverConn net.Conn
	for {
		serverConn, err = jumpConn.Dial("tcp", serverEndpoint.String())
		log.Debugf("Dial into SSHD Container %s", serverEndpoint.String())
		if err == nil {
			tunnelCreating.state = tunnelDone
			break
		}
		wait := exponentialBackoff.NextBackOff()
		log.Debugf("Backoff Tick %s", wait.String())
		time.Sleep(wait)
	}
	sshBoxConfig := &ssh.ClientConfig{
		User: "punch",
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

func tunnelStartingSpinner(lock *Semaphore, tunnelStatus *tunnelStatus) {
	go func() {
		if !lock.CanRun() {
			return
		}
		defer lock.Done()
		s := spin.New()
		for tunnelStatus.state == tunnelStarting {
			fmt.Printf("\rStarting tunnel %s ", s.Next())
			time.Sleep(100 * time.Millisecond)
		}
		if tunnelStatus.state == tunnelDone {
			fmt.Printf("\rStarting tunnel ")
			d := color.New(color.FgGreen, color.Bold)
			d.Printf("✔\n")
		}
	}()
}
func cleanup(config *Config) {
	fmt.Println("\nClosing tunnel")
	errSession := config.RestAPI.StartSession(config.RestAPI.RefreshToken)
	errDelete := config.RestAPI.DeleteTunnelAPI(config.Subdomain)
	if errSession != nil || errDelete != nil {
		fmt.Fprintf(os.Stderr,
			"We had some trouble deleting your tunnel. Use ulacli cleanup %s to make sure we know it's closed.\n", config.Subdomain)
	}
}
