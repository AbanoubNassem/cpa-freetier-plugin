package main

import (
	"errors"
	"net/http"
)

// upstreamHTTPError preserves a gateway rejection through the executor ABI.
type upstreamHTTPError struct {
	status  int
	message string
}

func (err *upstreamHTTPError) Error() string { return err.message }

func upstreamErrorStatus(err error) int {
	var upstream *upstreamHTTPError
	if errors.As(err, &upstream) {
		return upstream.status
	}
	return http.StatusBadGateway
}
