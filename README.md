# Sync Nexudus with Doorflow

This service is responsible for making sure Nexudus state is kept in sync with Doorflow.
This allows user signups and permissions set in Nexudus to be reflected in door access.

Data is received from Nexudus in two ways.

- The primary way is via webhooks, which are proxied to NATS to be easily handled by this service.
  This is done continuously, to sync data with Doorflow as it is updated in Nexudus.

- The secondary way is through the REST API.
  This is done in a batch fashion at start up, in order to synchronize state that was missed if the service goes down.

## Building

You should install the golang compiler, and then use the command `go build cmd/main.go`.

## Dependencies

The Nexudus client is generated from a swagger API definition.
If this needs to be regenerated, you can use the [go-swagger](https://github.com/go-swagger/go-swagger) tool.
The following commands will update the Nexudus client to the latest API definition:

```bash
# Generate the client
swagger generate client -f https://spaces.nexudus.com/swagger/docs/v1 -t ./internal/nexudus --skip-validation

# Make sure the required are available
go mod tidy
go mod get
```

