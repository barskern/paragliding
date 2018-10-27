package igcserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Convenience function to create a simple igc-file hosting server which hosts
// two files, one valid 'test.igc' and an invalid 'invalid.igc'
func makeIgcTestServer() *httptest.Server {
	return httptest.NewUnstartedServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.RequestURI == "/test.igc" {
				f, err := os.Open("../assets/test.igc")
				if err != nil {
					fmt.Printf("error when trying to read 'test.igc': %s", err)
				}
				_, err = io.Copy(w, f)
				if err != nil {
					fmt.Printf("error when trying to write file contents to response: %s", err)
				}
				fmt.Println("wrote valid igc content to response")
			} else if r.RequestURI == "/invalid.igc" {
				invalidIGC := "asljdkfjaøsljfølwer jfølvjasdløkv aøljsgødl v"
				w.Write([]byte(invalidIGC))
				fmt.Println("wrote invalid igc content to response")
			} else {
				http.Error(w, "not found", http.StatusNotFound)
				fmt.Println("wrote not found to response")
			}
		}),
	)
}

// Convenience function to create testdata to insert into the database
func makeTestData(serverURL string) []TrackMeta {
	return []TrackMeta{
		{
			time.Now(),
			"Aladin Special",
			"Magical Carpet",
			"MGI2",
			1200,
			serverURL + "/aladin.igc",
		},
		{
			time.Now(),
			"John Normal",
			"Boeng 777",
			"BG7",
			10,
			serverURL + "/boeng.igc",
		},
	}
}

// Test GET /
func TestIgcServerGetMetaValid(t *testing.T) {
	server := NewServer(nil)

	req := httptest.NewRequest("GET", "/", nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var data map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Errorf("received response body: '%s'", res.Body)
		t.Fatalf("failed when trying to decode body as json")
	}
	for _, field := range []string{"uptime", "info", "version"} {
		if data[field] == nil {
			t.Errorf("'%s' was not found in fields ", field)
		}
	}
}

// Test bad POST /track
func TestIgcServerPostTrackBad(t *testing.T) {

	// Setup a simple igc-file hosting server
	igcTestServer := makeIgcTestServer()
	igcTestServer.Start()
	defer igcTestServer.Close()

	server := NewServer(igcTestServer.Client())

	for _, body := range []string{
		fmt.Sprintf("{\"url\":\"%s\"}", igcTestServer.URL+"/invalid.igc"),
		fmt.Sprintf("{\"url\":\"%s\"}", igcTestServer.URL+"/missing.igc"),
		fmt.Sprintf("{\"url\":\"%s\"}", "asfd££@@1££¡@3invalidasULR"),
		"{\"url\":null}",
		"{\"url\":\"  a:b:c:d@a:b:©:1\"}",
		fmt.Sprintf("{\"l\":\"%s\"}", igcTestServer.URL+"/aa.igc"),
		fmt.Sprintf("\"l\":\"%s\"}", igcTestServer.URL+"/bb.igc"),
		fmt.Sprintf("{\"l\":\"%s\", asdf asdf}", igcTestServer.URL+"/cc.igc"),
	} {
		req := httptest.NewRequest("POST", "/track", bytes.NewReader([]byte(body)))
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)

		code := res.Result().StatusCode
		if code != 400 {
			t.Fatalf("expected '%s' to return 400 (bad request), got '%d'", body, code)
		}
	}
}

// Test valid POST /track
func TestIgcServerPostTrackValid(t *testing.T) {

	// Setup a simple igc-file hosting server
	igcTestServer := makeIgcTestServer()
	igcTestServer.Start()
	defer igcTestServer.Close()

	server := NewServer(igcTestServer.Client())

	body := fmt.Sprintf("{\"url\":\"%s\"}", igcTestServer.URL+"/test.igc")
	req := httptest.NewRequest("POST", "/track", bytes.NewReader([]byte(body)))
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var data map[string]TrackID
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Errorf("received response body: '%s'", res.Body)
		t.Fatalf("failed when trying to decode body as json")
	}

	req = httptest.NewRequest("GET", "/track", nil)
	res = httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var respData []TrackID
	if err := json.Unmarshal(res.Body.Bytes(), &respData); err != nil {
		t.Errorf("received response body: '%s'", res.Body)
		t.Fatalf("failed when trying to decode body as json")
	}

	for _, gotID := range respData {
		if gotID == data["id"] {
			return
		}
	}
	t.Fatalf("id of inserted track ('%d') was not found in ids returned from `GET /track` ('%d')", data["id"], respData)
}

// Test valid POST /track
func TestIgcServerPostTrackValidDuplicate(t *testing.T) {

	// Setup a simple igc-file hosting server
	igcTestServer := makeIgcTestServer()
	igcTestServer.Start()
	defer igcTestServer.Close()

	server := NewServer(igcTestServer.Client())

	body := fmt.Sprintf("{\"url\":\"%s\"}", igcTestServer.URL+"/test.igc")
	req := httptest.NewRequest("POST", "/track", bytes.NewReader([]byte(body)))
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var data map[string]TrackID
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Errorf("received response body: '%s'", res.Body)
		t.Fatalf("failed when trying to decode body as json")
	}

	req = httptest.NewRequest("POST", "/track", bytes.NewReader([]byte(body)))
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	code := res.Result().StatusCode
	if code != 403 {
		t.Fatalf("expected attempt to register same file twice to result in 403, got '%d'", code)
	}

}

// Test GET /track
func TestIgcServerGetTrack(t *testing.T) {
	server := NewServer(nil)

	testTrackMetas := makeTestData("localhost")
	ids := make([]TrackID, 0, len(testTrackMetas))
	for _, trackMeta := range testTrackMetas {
		id, err := server.data.Append(trackMeta)
		if err != nil {
			t.Errorf("unable to add metadata: %s", err)
			continue
		}
		ids = append(ids, id)
	}

	req := httptest.NewRequest("GET", "/track", nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	var data []TrackID
	if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
		t.Errorf("received response body: '%s'", res.Body)
		t.Fatalf("failed when trying to decode body as json")
	}

outer:
	for _, exptID := range ids {
		for _, gotID := range data {
			if TrackID(gotID) == exptID {
				continue outer
			}
		}
		t.Errorf("id of inserted track ('%d') was not found in ids returned from `GET /track` ('%d')", exptID, data)
	}
}

// Test valid GET /track/<id>
func TestIgcServerGetTrackByIdValid(t *testing.T) {
	server := NewServer(nil)

	testTrackMetas := makeTestData("localhost")
	ids := make([]TrackID, 0, len(testTrackMetas))
	for _, trackMeta := range testTrackMetas {
		meta, err := server.data.Append(trackMeta)
		if err != nil {
			t.Errorf("unable to add metadata: %s", err)
			continue
		}
		ids = append(ids, meta)
	}

	for i, id := range ids {
		uri := fmt.Sprintf("/track/%d", id)
		req := httptest.NewRequest("GET", uri, nil)
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)

		var data TrackMeta
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Errorf("received response body: '%s'", res.Body)
			t.Fatalf("failed when trying to decode body as json")
		}
		if !cmp.Equal(data, testTrackMetas[i]) {
			expt, _ := json.MarshalIndent(testTrackMetas[i], "", "  ")
			got, _ := json.MarshalIndent(data, "", "  ")
			t.Errorf("returned track was not equal to inserted track:\n\nrequested id: %d\nexpected:\n%s\n\nreturned:\n%s", id, expt, got)
		}
	}
}

// Test bad GET /track/<id>
func TestIgcServerGetTrackByIdBad(t *testing.T) {
	server := NewServer(nil)

	for _, badID := range []struct {
		int
		string
	}{
		{400, "aaaabbbb"},
		{400, "aaaabbbb/asdfa"},
		{400, "bad"},
		{400, "aøaskdljflkasdjfløjsdaf"},
		{400, "12312o3123"},
		{400, "--asdf--"},
		{400, "a"},
		{404, "1232"},
		{404, "99999"},
	} {
		req := httptest.NewRequest("GET", "/track/"+badID.string, nil)
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)

		code := res.Result().StatusCode
		if code != badID.int {
			t.Errorf("expected `GET /track/%s` to return '%d', got '%d'", badID.string, badID.int, code)
		}
	}
}

// Test valid GET /track/<id>/<field>
func TestIgcServerGetTrackFieldValid(t *testing.T) {
	server := NewServer(nil)

	testTrackMetas := makeTestData("localhost")
	ids := make([]TrackID, 0, len(testTrackMetas))
	for _, trackMeta := range testTrackMetas {
		meta, err := server.data.Append(trackMeta)
		if err != nil {
			t.Errorf("unable to add metadata: %s", err)
			continue
		}
		ids = append(ids, meta)
	}

	for i, id := range ids {
		// Encode and decode struct to make indexing easier
		exptJSON, _ := json.Marshal(testTrackMetas[i])
		var expt map[string]interface{}
		json.Unmarshal(exptJSON, &expt)

		for _, field := range []string{
			"pilot",
			"glider",
			"glider_id",
			"track_src_url",
		} {
			uri := fmt.Sprintf("/track/%d/%s", id, field)
			req := httptest.NewRequest("GET", uri, nil)
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			got, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("error when reading body: %s", err)
			}
			if string(got) != expt[field] {
				t.Errorf("unexpected field when `GET /track/%d/%s`, got '%s' but expected '%s'", id, field, got, expt[field])
			}

		}
		for _, field := range []string{
			"H_date",
			"track_length",
		} {
			uri := fmt.Sprintf("/track/%d/%s", id, field)
			req := httptest.NewRequest("GET", uri, nil)
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			got, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("error when reading body: %s", err)
			}
			if got == nil {
				t.Errorf("empty field when `GET /track/%d/%s`", id, field)
			}
			if string(got) == "" {
				t.Errorf("empty string when `GET /track/%d/%s`", id, field)
			}
		}
	}
}

// Test bad GET /track/<id>/<field>
func TestIgcServerGetTrackFieldBad(t *testing.T) {
	server := NewServer(nil)

	testTrackMetas := makeTestData("localhost")
	ids := make([]TrackID, 0, len(testTrackMetas))
	for _, trackMeta := range testTrackMetas {
		meta, err := server.data.Append(trackMeta)
		if err != nil {
			t.Errorf("unable to add metadata: %s", err)
			continue
		}
		ids = append(ids, meta)
	}

	var unknownID TrackID

	// It is a rare occurrence, but make sure that the unknown id NEVER can be
	// equal to a generated id
outer:
	for {
		unknownID = TrackID(rand.Int())
		for _, id := range ids {
			if unknownID == id {
				continue outer
			}
		}
		break
	}

	for _, data := range []struct {
		code  int
		field string
		id    TrackID
	}{
		{400, "asdlfkjaksl", ids[0]},
		{400, "aasdf90123", ids[0]},
		{400, "12312", ids[1]},
		{400, "--..s.a", ids[1]},
		{404, "asdf", unknownID},
	} {
		uri := fmt.Sprintf("/track/%d/%s", data.id, data.field)
		req := httptest.NewRequest("GET", uri, nil)
		res := httptest.NewRecorder()

		server.ServeHTTP(res, req)

		code := res.Result().StatusCode
		if code != data.code {
			t.Fatalf("expected `GET /track/%d/%s` to return '%d', got '%d'", data.id, data.field, data.code, code)
		}
	}
}

// Test different rubbish urls -> 404
func TestIgcServerGetRubbish(t *testing.T) {
	server := NewServer(nil)

	rubbishURLs := []string{
		"/rubbish",
		"/asdfa",
		"/asdfas/asdfasd/asdfasdf/asdfasdf/",
		"/paragliding/asdfasf",
		"/paragliding/igcasd",
		"/paragliding/api/asdlfasdf",
		"/paragliding/api/rubbish",
		"/paragliding/api/0a90a9ds109123",
		"/paragliding/api/some-path",
		"/012312390123123/api/some-path",
		"/a213asd123/api/some-path",
	}

	for _, rubbishURL := range rubbishURLs {
		req := httptest.NewRequest("GET", rubbishURL, nil)
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)

		code := res.Result().StatusCode
		if code != 404 {
			t.Fatalf("expected `GET %s` to return a 404, got %d", rubbishURL, code)
		}
	}
}

// Test PUT -> 405 response
func TestIgcServerPutMethod(t *testing.T) {
	server := NewServer(nil)

	req := httptest.NewRequest("PUT", "/", nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	code := res.Result().StatusCode
	if code != 405 {
		t.Fatalf("expected `PUT /` to return a 405, got '%d'", code)
	}
	allowedMethods := res.Result().Header.Get("Allow")
	if allowedMethods == "" {
		t.Fatalf("expected `PUT /` to return an `Allow` header containing the allowed methods, no methods returned or missing header")
	}
}
