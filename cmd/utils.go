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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)


func fixFilePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	return path
}

func getPublicKey(path string) (string, error) {
	path = fixFilePath(path)
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		reportError("ulacli requires an SSH private key to connect to our servers.  By default we do not use your existing keypair.  "+
			"You can point to an existing key-pair by editing ulacli.toml or generate a single-purpose key using `ulacli generate-key`", false)
		return "", err
	}
	return string(buf), nil
}

func reportError(err string, exit bool) {
	if err == "" {
		fmt.Fprintf(os.Stderr, "Unexpected error occured\n")
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", strings.ToUpper(string(err[0]))+err[1:])
	}
	if exit {
		os.Exit(1)
	}
}
