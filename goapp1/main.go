package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

var tracer trace.Tracer

func run() (err error) {

	initProvider()
	tracer = otel.Tracer("test-tracer")
	ctx := context.Background()
	// Start HTTP server.
	srv := &http.Server{
		Addr:         ":8080",
		BaseContext:  func(_ net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      newHTTPHandler(),
	}
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.ListenAndServe()
	}()

	log.Printf("Up, wait for shutdown..")
	// Wait for interruption.
	select {
	case err = <-srvErr:
		// Error when starting HTTP server.
		return
	}

}

func newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// handleFunc is a replacement for mux.HandleFunc
	// which enriches the handler's HTTP instrumentation with the pattern as the http.route.
	handleFunc := func(pattern string, handlerFunc func(http.ResponseWriter, *http.Request)) {
		// Configure the "http.route" for the HTTP instrumentation.
		handler := otelhttp.WithRouteTag(pattern, http.HandlerFunc(handlerFunc))
		mux.Handle(pattern, handler)
	}

	// Register handlers.
	handleFunc("/rolldice", rolldice)
	handleFunc("/", ping)
	handleFunc("/dapr/subscribe", subscribe)

	// Add HTTP instrumentation for the whole server.
	handler := otelhttp.NewHandler(mux, "/")
	return handler
}

func subscribe(w http.ResponseWriter, r *http.Request) {
	log.Printf("Got sub request")
	w.WriteHeader(http.StatusNotFound)
}

type SomeStruct struct {
	Name string `json:"name"`
}

func ping(w http.ResponseWriter, r *http.Request) {
	svcName := os.Getenv("SVC_NAME")
	log.Printf("received req in %s", r.URL.Path)
	log.Printf("%s: Received CE", svcName)
	for k, v := range r.Header {
		log.Printf(" %v: %v", k, v)
	}
	commonAttrs := []attribute.KeyValue{
		attribute.String("svcName", svcName),
		attribute.String("attrB", "raspberry"),
		attribute.String("attrC", "vanilla"),
	}

	ctx := r.Context()
	uk := attribute.Key("username")
	span0 := trace.SpanFromContext(ctx)
	bag := baggage.FromContext(ctx)
	span0.AddEvent("handling this...", trace.WithAttributes(uk.String(bag.Member("username").Value())))
	defer span0.End()

	ctx, span := tracer.Start(
		ctx,
		"CollectorExporter-Example",
		trace.WithAttributes(commonAttrs...))

	for i := 0; i < 2; i++ {
		_, span2 := tracer.Start(
			ctx,
			"Nested 1")
		time.Sleep(100 * time.Millisecond)
		span2.End()
	}

	defer span.End()
	// Reply
	newCeType := os.Getenv("CE_REPLY")
	if newCeType == "" {
		newCeType = "fran.pingreply"
	}
	uuidN, _ := uuid.NewRandom()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Ce-Id", uuidN.String())
	w.Header().Set("Ce-Specversion", "1.0")
	w.Header().Set("Ce-Source", svcName)
	w.Header().Set("Ce-Type", newCeType)

	//ce-id:1 ce-specversion:1.0 ce-source:fran ce-type:fran.

	w.WriteHeader(http.StatusOK)
	data := SomeStruct{
		Name: "Juan",
	}
	json.NewEncoder(w).Encode(data)

	sendDapr(r)
}

func sendDapr(r *http.Request) {
	//res, _ := http.Post("http://localhost:3500/v1.0/publish/pubsub/pingreply")
	requestURL := "http://localhost:3500/v1.0/publish/pubsub/pingreply"
	jsonBody := []byte(`{"client_message": "hello, server!"}`)
	bodyReader := bytes.NewReader(jsonBody)
	traceParentv := r.Header["Traceparent"]
	traceParent := "00-00000000000000000000000000000000-0000000000000000-00"
	if len(traceParentv) > 0 {
		traceParent = traceParentv[0]
	}

	req, _ := http.NewRequest(http.MethodPost, requestURL, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Traceparent", traceParent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Do err: %s", err)
		return
	}
	resBody, _ := io.ReadAll(res.Body)
	log.Printf("Ret %d: %s", res.StatusCode, resBody)

}
