package fronius

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientRequestErrors(t *testing.T) {
	methods := map[string]func(*SymoClient) error{
		"power":    func(c *SymoClient) error { _, err := c.GetPowerFlowData(); return err },
		"archive":  func(c *SymoClient) error { _, err := c.GetArchiveData(); return err },
		"inverter": func(c *SymoClient) error { _, err := c.GetInverterRealtimeData(); return err },
		"meter":    func(c *SymoClient) error { _, err := c.GetMeterRealtimeData(); return err },
		"storage":  func(c *SymoClient) error { _, err := c.GetStorageRealtimeData(); return err },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("invalid json")) }))
	defer server.Close()
	for name, method := range methods {
		t.Run(name+"/invalid JSON", func(t *testing.T) {
			c, err := NewSymoClient(ClientOptions{URL: server.URL})
			require.NoError(t, err)
			require.Error(t, method(c))
		})
		t.Run(name+"/invalid URL", func(t *testing.T) {
			c, err := NewSymoClient(ClientOptions{URL: "://invalid"})
			require.NoError(t, err)
			require.Error(t, method(c))
		})
	}
}

func TestClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c, err := NewSymoClient(ClientOptions{URL: server.URL, Timeout: 20 * time.Millisecond})
	require.NoError(t, err)
	_, err = c.GetPowerFlowData()
	require.Error(t, err)
}

func TestConcurrentRequestsUseCorrectPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PowerDataPath:
			_, _ = w.Write([]byte(`{"Body":{"Data":{"Site":{"P_Grid":123}}}}`))
		case "/solar_api/v1/GetStorageRealtimeData.cgi":
			_, _ = w.Write([]byte(`{"Body":{"Data":{"0":{"Controller":{"Temperature_Cell":42}}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, err := NewSymoClient(ClientOptions{URL: server.URL})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			data, err := c.GetPowerFlowData()
			if err != nil {
				t.Error(err)
				return
			}
			if data.Site.PowerGrid != 123 {
				t.Errorf("power grid = %v", data.Site.PowerGrid)
			}
		}()
		go func() {
			defer wg.Done()
			data, err := c.GetStorageRealtimeData()
			if err != nil {
				t.Error(err)
				return
			}
			if data.Controller.Temperature_Cell != 42 {
				t.Errorf("temperature = %v", data.Controller.Temperature_Cell)
			}
		}()
	}
	wg.Wait()
}

func TestClientSendsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"Body":{"Data":{}}}`))
	}))
	defer server.Close()
	c, err := NewSymoClient(ClientOptions{URL: server.URL, Headers: http.Header{"Authorization": []string{"Bearer test"}}})
	require.NoError(t, err)
	_, err = c.GetPowerFlowData()
	require.NoError(t, err)
}

func TestClientConnectionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	c, err := NewSymoClient(ClientOptions{URL: server.URL})
	require.NoError(t, err)
	_, err = c.GetPowerFlowData()
	require.Error(t, err)
}
