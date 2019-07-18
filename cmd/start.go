// UserLAnd Cloud CLI
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

package cmd

import (
	"net/url"
	"os"
	"github.com/spf13/cobra"
	"github.com/cypherpunkarmory/ulacli/box"
)

// httpCmd represents the http command
var httpCmd = &cobra.Command{
	Use:   "start",
	Short: "s",
	Long:  "start",
	Run: func(cmd *cobra.Command, args []string) {
		startBox()
	},
}

func init() {
	rootCmd.AddCommand(httpCmd)
}

func startBox() {
	publicKey, err := getPublicKey(publicKeyPath)
	if err != nil {
		os.Exit(3)
	}

	response, err := restAPI.CreateBoxAPI(publicKey)

	if err != nil {
		reportError(err.Error(), true)
	}

	connectionURL, err := url.Parse(sshEndpoint)
	if err != nil {
		reportError("The ssh endpoint is not a valid URL", true)
		os.Exit(3)
	}

	boxConfig := box.Config{
		ConnectionEndpoint: *connectionURL,
		RestAPI:            restAPI,
		Box:        		response,
		PrivateKeyPath:     privateKeyPath,
		LocalPort:          port,
		LogLevel:           logLevel,
	}
	semaphore := box.Semaphore{}
	box.StartBox(&boxConfig, nil, &semaphore)
}
