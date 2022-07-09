package main

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

var isBeingTested bool
var vazao = make([]uint32, 20)

type Executor struct {
	cancel    context.CancelFunc
	logFile   *os.File
	batchLat  time.Time
	measuring bool
	thrCount  uint32 // atomic
	t         *time.Ticker
}

func NewExecutor() (*Executor, error) {
	ctx, cn := context.WithCancel(context.Background())
	fn := "/data/logfile.log"
	flags := os.O_CREATE | os.O_TRUNC | os.O_WRONLY | os.O_APPEND | os.O_SYNC
	ex := &Executor{
		cancel: cn,
		t:      time.NewTicker(time.Second),
	}
	var err error
	ex.logFile, err = os.OpenFile(fn, flags, 0600)
	if err != nil {
		panic(err)
	}

	go ex.monitorThroughput(ctx)
	return ex, nil
}

func (ex *Executor) monitorThroughput(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ex.t.C:
			t := atomic.SwapUint32(&ex.thrCount, 0)
			vazao = append(vazao, t)
		}
	}
}

func main() {
	isBeingTested = false
	app := fiber.New(fiber.Config{
		Concurrency: 1024 * 1024 * 1024,
		AppName:     "Test App v1.0.1",
	})
	var keyValueDB map[int]string
	keyValueDB = make(map[int]string)

	ex, err := NewExecutor()
	if err != nil {
		panic(err)
	}

	app.Get("/", func(c *fiber.Ctx) error {
		key, err := strconv.Atoi(c.Query("key"))
		if err != nil {
			return c.Status(500).JSON("Não foi possível converter a entrada para numérico")
		}
		result, ok := keyValueDB[key]
		if ok {
			atomic.AddUint32(&ex.thrCount, 1)
			return c.JSON(result)
		}
		atomic.AddUint32(&ex.thrCount, 1)
		return c.Status(404).JSON("Key not found")
	})

	app.Post("/", func(c *fiber.Ctx) error {
		payload := struct {
			Key   int    `json:"key"`
			Value string `json:"value"`
		}{}

		if err := c.BodyParser(&payload); err != nil {
			atomic.AddUint32(&ex.thrCount, 1)
			return c.Status(400).JSON("Error when trying to decode the payload")
		}
		keyValueDB[payload.Key] = payload.Value
		atomic.AddUint32(&ex.thrCount, 1)
		return c.Status(204).JSON("")
	})

	app.Post("/testing", func(c *fiber.Ctx) error {
		payload := struct {
			Action string `json:"action"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON("Error when trying to decode the payload")
		}
		if payload.Action == "start" {
			isBeingTested = true
		}
		if payload.Action == "stop" {
			isBeingTested = false

			for _, v := range vazao {
				_, err := fmt.Fprintf(ex.logFile, "%d\n", v)
				if err != nil {
					log.Print(err)
					return err
				}
			}
		}

		return c.Status(204).JSON("")
	})

	app.Delete("/", func(c *fiber.Ctx) error {
		key, err := strconv.Atoi(c.Query("key"))
		if err != nil {
			return c.Status(500).JSON("Não foi possível converter a entrada para numérico")
		}
		delete(keyValueDB, key)
		atomic.AddUint32(&ex.thrCount, 1)
		return c.Status(204).JSON("")
	})

	var portEnv = os.Getenv("PORT")
	if portEnv == "" {
		panic("Couldn't find PORT env")
	}
	err = app.Listen(portEnv)
	if err != nil {
		panic(err)
	}
}
