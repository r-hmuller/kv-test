package main

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

type Executor struct {
	cancel context.CancelFunc
	//logFile   *os.File
	batchLat  time.Time
	measuring bool
	thrCount  uint32 // atomic
	t         *time.Ticker
}

func NewExecutor() (*Executor, error) {
	ctx, cn := context.WithCancel(context.Background())
	ex := &Executor{
		cancel: cn,
		t:      time.NewTicker(time.Second),
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
			println(t)
		}
	}
}

func main() {
	app := fiber.New()
	var keyValueDB map[string]string
	keyValueDB = make(map[string]string)

	ex, err := NewExecutor()
	if err != nil {
		panic(err)
	}

	app.Get("/", func(c *fiber.Ctx) error {
		key := c.Query("key")
		result, ok := keyValueDB[key]
		if ok {
			atomic.AddUint32(&ex.thrCount, 1)
			return c.JSON(result)
		}
		atomic.AddUint32(&ex.thrCount, 1)
		log.Print(&ex.thrCount)
		return c.Status(404).JSON("Key not found, and counter is ->" + strconv.Itoa(int(ex.thrCount)))
	})

	app.Post("/", func(c *fiber.Ctx) error {
		payload := struct {
			Key   string `json:"key"`
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

	app.Delete("/", func(c *fiber.Ctx) error {
		key := c.Query("key")
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
