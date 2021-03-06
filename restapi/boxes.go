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

package restapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/google/jsonapi"
)

//Box JSONAPI response of box object
type Box struct {
	ID        string     `jsonapi:"primary,box"`
	Port      []string   `jsonapi:"attr,port,omitempty"`
	PublicKey string     `jsonapi:"attr,sshKey,omitempty"`
	Image     string     `jsonapi:"attr,image,omitempty"`
	SSHPort   string     `jsonapi:"attr,sshPort,omitempty"`
	IPAddress string     `jsonapi:"attr,ipAddress,omitempty"`
	Config *Config `jsonapi:"relation,config,omitempty"`
}

//CreateBoxAPI calls UserLAnd Cloud web api to get box details
func (restClient *RestClient) CreateBoxAPI(publicKey string, image string) (Box, error) {
	boxReturn := Box{}
	var outputBuffer bytes.Buffer

	request := Box{
		PublicKey: publicKey,
		Image: image,
	}

	_ = bufio.NewWriter(&outputBuffer)
	err := jsonapi.MarshalPayload(&outputBuffer, &request)
	if err != nil {
		return boxReturn, errorUnableToParse
	}

	url := restClient.URL + "/boxes"
	req, err := http.NewRequest("POST", url, &outputBuffer)
	if err != nil {
		return boxReturn, errorCantConnectRestCall
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := restClient.Client.Do(req)
	if err != nil {
		return boxReturn, errorCantConnectRestCall
	}
	defer resp.Body.Close()

	if resp.StatusCode > 399 {
		buf, _ := ioutil.ReadAll(resp.Body)
		errObject := ResponseError{}
		err = json.Unmarshal(buf, &errObject)
		if err != nil {
			return boxReturn, err
		}
		return boxReturn, &errObject
	}

	err = jsonapi.UnmarshalPayload(resp.Body, &boxReturn)
	if err != nil {
		return boxReturn, errorUnableToParse
	}
	return boxReturn, nil
}

//DeleteBoxAPI deletes box
func (restClient *RestClient) DeleteBoxAPI(boxId string) error {

	url := restClient.URL + "/boxes/" + boxId
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return errorCantConnectRestCall
	}
	resp, err := restClient.Client.Do(req)
	if err != nil {
		return errorCantConnectRestCall
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		return nil
	}

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode > 399 {
		errorBody := ResponseError{}
		err = json.Unmarshal(body, &errorBody)
		if err != nil {
			return err
		}
		return &errorBody
	}

	return errorUnableToDelete
}
