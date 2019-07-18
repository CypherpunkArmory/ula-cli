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
	"reflect"

	"github.com/google/jsonapi"
)

//Box JSONAPI response of box object
type Box struct {
	ID        string     `jsonapi:"primary,box"`
	Port      []string   `jsonapi:"attr,port,omitempty"`
	PublicKey string     `jsonapi:"attr,sshKey,omitempty"`
	SSHPort   string     `jsonapi:"attr,sshPort,omitempty"`
	IPAddress string     `jsonapi:"attr,ipAddress,omitempty"`
	Subdomain *Subdomain `jsonapi:"relation,config,omitempty"`
}

//CreateBoxAPI calls UserLAnd Cloud web api to get box details
func (restClient *RestClient) CreateBoxAPI(subdomain string, publicKey string, protocol []string) (Box, error) {
	boxReturn := Box{}
	var outputBuffer bytes.Buffer

	if subdomain != "" {
		subdomainID, err := restClient.getSubdomainID(subdomain)
		if err != nil {
			return boxReturn, errorUnownedSubdomain
		}
		request := Box{
			Port:      protocol,
			PublicKey: publicKey,
			Subdomain: &Subdomain{
				ID: subdomainID,
			},
		}
		_ = bufio.NewWriter(&outputBuffer)
		err = jsonapi.MarshalPayload(&outputBuffer, &request)
		if err != nil {
			return boxReturn, errorUnableToParse
		}
	} else {
		request := Box{
			Port:      protocol,
			PublicKey: publicKey,
		}

		_ = bufio.NewWriter(&outputBuffer)
		err := jsonapi.MarshalPayload(&outputBuffer, &request)
		if err != nil {
			return boxReturn, errorUnableToParse
		}
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
func (restClient *RestClient) DeleteBoxAPI(subdomainName string) error {
	id, err := restClient.getBoxID(subdomainName)
	if err != nil {
		return err
	}

	url := restClient.URL + "/boxes/" + id
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
func (restClient *RestClient) getBoxID(subdomainName string) (string, error) {
	url := restClient.URL + "/boxes?filter[config][name]=" + subdomainName
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", errorCantConnectRestCall
	}
	resp, err := restClient.Client.Do(req)
	if err != nil {
		return "", errorCantConnectRestCall
	}
	defer resp.Body.Close()
	if resp.StatusCode > 399 {
		buf, _ := ioutil.ReadAll(resp.Body)
		errObject := ResponseError{}
		err = json.Unmarshal(buf, &errObject)
		if err != nil {
			return "", err
		}
		return "", &errObject
	}

	boxes, err := jsonapi.UnmarshalManyPayload(resp.Body, reflect.TypeOf(new(Box)))
	if err != nil {
		return "", errorUnableToParse
	}
	if len(boxes) == 0 {
		return "", errorUnownedBox
	}
	t, _ := boxes[0].(*Box)
	if t.ID == "" {
		return "", errorUnownedBox
	}
	return t.ID, nil
}
