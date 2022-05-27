/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package openwhisk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

// field name of user logs
const LOG_FIELD = "__OW_LOGS"

// ErrResponse is the response when there are errors
type ErrResponse struct {
	Error string `json:"error"`
}

func sendError(w http.ResponseWriter, code int, cause string) {
	errResponse := ErrResponse{Error: cause}
	b, err := json.Marshal(errResponse)
	if err != nil {
		b = []byte("error marshalling error response")
		Debug(err.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(b)
	w.Write([]byte("\n"))
}

func (ap *ActionProxy) runHandler(w http.ResponseWriter, r *http.Request) {

	// parse the request
	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("Error reading request body: %v", err))
		return
	}
	Debug("done reading %d bytes", len(body))

	// check if you have an action
	if ap.theExecutor == nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("no action defined yet"))
		return
	}
	// check if the process exited
	if ap.theExecutor.Exited() {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("command exited"))
		return
	}

	// remove newlines
	body = bytes.Replace(body, []byte("\n"), []byte(""), -1)

	// read logs until two sentinel logs are got, this guarantee that all logs will be captured before send back to user
	stopSignal := make(chan bool)
	var logs []string
	go func() {
		var sentinel = 0
		for log := range ap.theExecutor.logger {
			if strings.Contains(log, OutputGuardRaw) {
				sentinel += 1
			}
			if sentinel == 2 {
				break
			}
			logs = append(logs, log)
		}
		stopSignal <- true
	}()

	// execute the action
	response, err := ap.theExecutor.Interact(body)

	// check for early termination
	if err != nil {
		Debug("WARNING! Command exited")
		ap.outFile.Write([]byte(OutputGuard))
		ap.errFile.Write([]byte(OutputGuard))
		ap.theExecutor = nil
		sendError(w, http.StatusBadRequest, fmt.Sprintf("command exited"))
		return
	}
	DebugLimit("received:", response, 120)

	// check if the answer is an object map
	var objmap map[string]interface{}
	var objarray []interface{}
	var ismap = true
	err = json.Unmarshal(response, &objmap)
	if err != nil {
		ismap = false
		err = json.Unmarshal(response, &objarray)
		if err != nil {
			sendError(w, http.StatusBadGateway, "The action did not return a dictionary or array.")
			return
		}
	}

	// wait for log reading finished
	<-stopSignal
	var newResponse []byte
	if ismap {
		objmap[LOG_FIELD] = logs
		newResponse, _ = json.Marshal(objmap)
	} else {
		newResponse, _ = json.Marshal(objarray)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(newResponse)))
	numBytesWritten, err := w.Write(newResponse)

	// flush output
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// diagnostic when you have writing problems
	if err != nil {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Error writing response: %v", err))
		return
	}
	if numBytesWritten != len(newResponse) {
		sendError(w, http.StatusInternalServerError, fmt.Sprintf("Only wrote %d of %d bytes to response", numBytesWritten, len(response)))
		return
	}
}
