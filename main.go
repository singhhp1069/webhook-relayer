package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

type key int

const (
	requestIDKey key = 0
)

var (
	listenAddr           string
	healthy              int32
	messageBufferSize    int
	maxRequestsPerSecond int
	queues               map[string]chan string
)

type Payload struct {
	Source string
	Data   []byte
}

func main() {
	flag.StringVar(&listenAddr, "listen", ":2110", "server listen address")
	flag.IntVar(&maxRequestsPerSecond, "x", 10, "Max-Requests-Per-Seconds: define the throttle limit in requests per seconds")
	flag.IntVar(&messageBufferSize, "n", 4096, "Max number of messages that are kept per agent")
	flag.Parse()

	// setup server
	e := echo.New()
	e.Use(middleware.Logger())
	e.HideBanner = true
	e.StdLogger.Println("starting cosmos-cash-agent-relayer rest server")
	e.StdLogger.Println("listen address is ", listenAddr)
	e.StdLogger.Println("Max messages for agent ", messageBufferSize)

	// start the rest server
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))

	e.Use(middleware.RateLimiter(
		middleware.NewRateLimiterMemoryStore(rate.Limit(maxRequestsPerSecond)),
	))

	// initialize the queues
	queues = make(map[string]chan string)
	dispatcherQueue := make(chan Payload)
	// start the dispatcher
	go func(dq chan Payload) {
		for {
			p := <-dq
			e.StdLogger.Println("sender: ", p.Source)
			q, exists := queues[p.Source]
			if !exists {
				q = make(chan string, messageBufferSize)
				queues[p.Source] = q
			}
			// if the queue is full drop incoming messages
			if cap(q) == len(q) {
				e.StdLogger.Println(p.Source, "queue full, dropping incoming message:", string(p.Data))
				return
			}
			q <- string(p.Data)
			e.StdLogger.Println("enqueuing event from", p.Source, " - new queue size", len(q))
		}
	}(dispatcherQueue)

	// register endpoints
	// webhook endpoint
	e.POST("/wh/:agent", func(c echo.Context) error {
		agent := c.Param("agent")
		defer c.Request().Body.Close()
		bodyBytes, err := ioutil.ReadAll(c.Request().Body)
		if err != nil {
			e.StdLogger.Println(err)
		}
		dispatcherQueue <- Payload{agent, bodyBytes}
		// track the resolution
		// atomic.AddUint64(&rt.resolves, 1)
		return c.JSON(http.StatusOK, map[string]string{})
	})
	// messages endpoint
	e.GET("/messages/:agent", func(c echo.Context) error {
		agent := c.Param("agent")
		q, found := queues[agent]
		if !found || len(q) == 0 {
			return c.JSON(http.StatusOK, []string{})
		}
		var sb strings.Builder
		prefix := "["
		for len(q) > 0 {
			sb.WriteString(prefix)
			sb.WriteString(<-q)
			prefix = ","
		}
		sb.WriteString("]")

		// track the resolution
		// atomic.AddUint64(&rt.resolves, 1)
		return c.Blob(http.StatusOK, "application/json", []byte(sb.String()))
	})
	// start the server
	e.StdLogger.Fatal(e.Start(listenAddr))
}
