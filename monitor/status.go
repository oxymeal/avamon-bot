package monitor

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// StatusType describes availability status as one enumerable value.
type StatusType uint

const (
	// StatusOK - the service is available.
	StatusOK = iota
	// StatusGenericError - some generic network error.
	StatusGenericError
	// StatusTimeout - HTTP request timed out.
	StatusTimeout
	// StatusURLParsingError - request URL is invalid.
	StatusURLParsingError
	// StatusDNSLookupError - could not resolve domain name.
	StatusDNSLookupError
	// StatusHTTPError - the service returned non-successfull HTTP status code.
	StatusHTTPError
)

func (st StatusType) String() string {
	switch st {
	case StatusOK:
		return "OK"
	case StatusGenericError:
		return "Generic Error"
	case StatusTimeout:
		return "Timeout"
	case StatusURLParsingError:
		return "URL Parsing Error"
	case StatusDNSLookupError:
		return "DNS Error"
	case StatusHTTPError:
		return "HTTP Error"
	}
	return "Unknown"
}

// Status contains information about service availability.
type Status struct {
	Type StatusType
	// The exact error value, if Type is not StatusOK.
	// If Type is StatusOK it is nil.
	Err error
	// Time spent for the request.
	ResponseTime time.Duration
	// If HTTP response was not received, it's set to 0.
	HTTPStatusCode int
	HTTPStatusText string
}

func (s *Status) String() string {
	var errText string
	if s.Err != nil {
		errText = fmt.Sprintf("%T: %q", s.Err, s.Err)
	} else {
		errText = "nil"
	}

	template := `Status {
  Type = %v,
  Err = %v,
  Response Time = %v,
  HTTP Status = %v %v,
}`
	return fmt.Sprintf(
		template,
		s.Type, errText, s.ResponseTime, s.HTTPStatusCode, s.HTTPStatusText,
	)
}

func newSuccessStatus(resp *http.Response, dur time.Duration) *Status {
	return &Status{
		Type:           StatusOK,
		Err:            nil,
		ResponseTime:   dur,
		HTTPStatusCode: resp.StatusCode,
		HTTPStatusText: http.StatusText(resp.StatusCode),
	}
}

func newGenericErrorStatus(err error, dur time.Duration) *Status {
	return &Status{
		Type:           StatusGenericError,
		Err:            err,
		ResponseTime:   dur,
		HTTPStatusCode: 0,
		HTTPStatusText: "",
	}
}

func newTimeoutStatus(err error, dur time.Duration) *Status {
	return &Status{
		Type:           StatusTimeout,
		Err:            err,
		ResponseTime:   dur,
		HTTPStatusCode: 0,
		HTTPStatusText: "",
	}
}

func newURLParsingErrorStatus(err error, dur time.Duration) *Status {
	return &Status{
		Type:           StatusURLParsingError,
		Err:            err,
		ResponseTime:   dur,
		HTTPStatusCode: 0,
		HTTPStatusText: "",
	}
}

func newDNSLookupErrorStatus(err error, dur time.Duration) *Status {
	return &Status{
		Type:           StatusDNSLookupError,
		Err:            err,
		ResponseTime:   dur,
		HTTPStatusCode: 0,
		HTTPStatusText: "",
	}
}

func newHTTPErrorStatus(resp *http.Response, dur time.Duration) *Status {
	return &Status{
		Type:           StatusHTTPError,
		Err:            fmt.Errorf("Server returned status '%v'", resp.Status),
		ResponseTime:   dur,
		HTTPStatusCode: resp.StatusCode,
		HTTPStatusText: http.StatusText(resp.StatusCode),
	}
}

func netErrToStatus(err error, dur time.Duration) *Status {
	if err, ok := err.(net.Error); ok {
		if err.Timeout() {
			return newTimeoutStatus(err, dur)
		}
	}
	if err, ok := err.(*url.Error); ok {
		if err, ok := err.Err.(*net.OpError); ok {
			if err, ok := err.Err.(*net.DNSError); ok {
				return newDNSLookupErrorStatus(err, dur)
			}
		}
	}

	return newGenericErrorStatus(err, dur)
}