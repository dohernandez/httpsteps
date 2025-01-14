package httpsteps_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bool64/httpmock"
	"github.com/cucumber/godog"
	httpsteps "github.com/godogx/httpsteps"
	"github.com/stretchr/testify/assert"
)

func TestLocal_RegisterSteps(t *testing.T) {
	mock, srvURL := httpmock.NewServer()
	mock.OnError = func(err error) {
		assert.NoError(t, err)
	}

	defer mock.Close()

	concurrencyLevel := 5
	setExpectations(mock, concurrencyLevel)

	local := httpsteps.NewLocalClient(srvURL, func(client *httpmock.Client) {
		client.Headers = map[string]string{
			"X-Foo": "bar",
		}
		client.ConcurrencyLevel = concurrencyLevel
	})
	local.AddService("some-service", srvURL)

	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			local.RegisterSteps(s)
		},
		Options: &godog.Options{
			Format: "pretty",
			Strict: true,
			Paths:  []string{"_testdata/LocalClient.feature"},
		},
	}

	if suite.Run() != 0 {
		t.Fatal("test failed")
	}

	assert.NoError(t, mock.ExpectationsWereMet())
}

func setExpectations(mock *httpmock.Server, concurrencyLevel int) {
	mock.Expect(httpmock.Expectation{
		Method:       http.MethodGet,
		RequestURI:   "/get-something?foo=bar",
		ResponseBody: []byte(`[{"some":"json"}]`),
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	})

	mock.Expect(httpmock.Expectation{
		Method:     http.MethodDelete,
		RequestURI: "/bad-request",
		RequestHeader: map[string]string{
			"X-Foo": "bar",
		},
		RequestCookie: map[string]string{
			"c1": "v1",
			"c2": "v2",
		},
		ResponseBody: []byte(`{"error":"oops"}`),
		Status:       http.StatusBadRequest,
	})

	mock.Expect(httpmock.Expectation{
		Method:     http.MethodPost,
		RequestURI: "/with-body",
		RequestHeader: map[string]string{
			"X-Foo": "bar",
		},
		RequestBody:  []byte(`[{"some":"json"}]`),
		ResponseBody: []byte(`{"status":"ok"}`),
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	})

	del := httpmock.Expectation{
		Method:     http.MethodDelete,
		RequestURI: "/delete-something",
		Status:     http.StatusNoContent,
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	}

	// Expecting 2 similar requests.
	mock.Expect(del)
	mock.Expect(del)

	// Due to idempotence testing several more requests should be expected.
	delNotFound := del
	delNotFound.Status = http.StatusNotFound
	delNotFound.ResponseBody = []byte(`{"status":"failed"}`)

	for i := 0; i < concurrencyLevel-1; i++ {
		mock.Expect(delNotFound)
	}

	// Expecting request containing json5 comments.
	mock.Expect(httpmock.Expectation{
		Method:     http.MethodPost,
		RequestURI: "/with-json5-body",
		RequestHeader: map[string]string{
			"X-Foo": "bar",
		},
		RequestBody:  []byte(`[{"some":"json5"}]`),
		ResponseBody: []byte(`{"status":"ok"}`),
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	})

	// Expecting request does not contain a valid json.
	mock.Expect(httpmock.Expectation{
		Method:       http.MethodGet,
		RequestURI:   "/with-csv-body",
		RequestBody:  []byte(`a,b,c`),
		ResponseBody: []byte(`a,b,c`),
	})

	// Expecting request for "Successful call against named service".
	mock.Expect(httpmock.Expectation{
		Method:       http.MethodGet,
		RequestURI:   "/get-something?foo=bar",
		ResponseBody: []byte(`[{"some":"json"}]`),
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	})
}

func TestLocal_RegisterSteps_unexpectedOtherResp(t *testing.T) {
	mock, srvURL := httpmock.NewServer()
	mock.OnError = func(err error) {
		assert.NoError(t, err)
	}

	defer mock.Close()

	concurrencyLevel := 5
	del := httpmock.Expectation{
		Method:     http.MethodDelete,
		RequestURI: "/delete-something",
		Status:     http.StatusNoContent,
		ResponseHeader: map[string]string{
			"Content-Type": "application/json",
		},
	}

	mock.Expect(del)

	// Due to idempotence testing several more requests should be expected.
	delNotFound := del
	delNotFound.Status = http.StatusNotFound
	delNotFound.ResponseBody = []byte(`{"status":"failed"}`)

	for i := 0; i < concurrencyLevel-1; i++ {
		mock.Expect(delNotFound)
	}

	local := httpsteps.NewLocalClient(srvURL, func(client *httpmock.Client) {
		client.ConcurrencyLevel = concurrencyLevel
	})
	out := bytes.NewBuffer(nil)

	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			local.RegisterSteps(s)
		},
		Options: &godog.Options{
			Output:   out,
			Format:   "pretty",
			NoColors: true,
			Strict:   true,
			Paths:    []string{"_testdata/LocalClientFail1.feature"},
		},
	}

	assert.Equal(t, 1, suite.Run())
	assert.NoError(t, mock.ExpectationsWereMet())
	assert.Contains(t, out.String(), "Error: after scenario hook failed: no other responses expected for default: unexpected response status, expected: 204 (No Content), received: 404 (Not Found)")
}

func TestLocal_RegisterSteps_dynamic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, err := w.Write([]byte(`{"id":12345,"name": "John Doe","created_at":"any","updated_at": "any"}`))
			assert.NoError(t, err)

			return
		}

		if r.URL.Path == "/order" {
			b, err := ioutil.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NoError(t, r.Body.Close())

			assert.Equal(t, `{"user_id":12345,"item_name":"Watermelon"}`, string(b))

			_, err = w.Write([]byte(`{"id":54321,"created_at":"any","updated_at": "any","user_id":12345}`))
			assert.NoError(t, err)

			return
		}
	}))
	defer srv.Close()

	local := httpsteps.NewLocalClient(srv.URL)

	suite := godog.TestSuite{
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			local.RegisterSteps(s)
		},
		Options: &godog.Options{
			Format: "pretty",
			Strict: true,
			Paths:  []string{"_testdata/Dynamic.feature"},
		},
	}

	if suite.Run() != 0 {
		t.Fatal("test failed")
	}
}
