package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var isBeingTested bool
var vazao = make([]uint32, 300)

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

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

var concurrentMap sync.Map

func main() {
	isBeingTested = false
	app := fiber.New(fiber.Config{
		Concurrency: 1024 * 1024 * 10,
		AppName:     "Test App v1.0.1",
	})

	ex, err := NewExecutor()
	if err != nil {
		panic(err)
	}

	app.Get("/", func(c *fiber.Ctx) error {
		key, err := strconv.Atoi(c.Query("key"))
		if err != nil {
			return c.Status(500).JSON("Não foi possível converter a entrada para numérico")
		}
		value, ok := concurrentMap.Load(key)
		if ok {
			atomic.AddUint32(&ex.thrCount, 1)
			return c.JSON(value)
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
		concurrentMap.Store(payload.Key, payload.Value)
		atomic.AddUint32(&ex.thrCount, 1)
		return c.Status(204).JSON("")
	})

	app.Post("/testing", func(c *fiber.Ctx) error {
		payload := struct {
			Action     string `json:"action"`
			Path       string `json:"path"`
			DumpMemory string `json:"dumpMemory"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON("Error when trying to decode the payload")
		}
		if payload.Action == "start" {
			isBeingTested = true
			vazao = make([]uint32, 300)
		}
		if payload.Action == "stop" {
			isBeingTested = false
			flags := os.O_CREATE | os.O_TRUNC | os.O_WRONLY | os.O_APPEND | os.O_SYNC
			customLog, err := os.OpenFile(payload.Path, flags, 0600)
			if err != nil {
				log.Print(err)
				return c.Status(500).JSON(err.Error())
			}

			dumpMemoryFile, err := os.OpenFile(payload.DumpMemory, flags, 0600)
			if err != nil {
				log.Print(err)
				return c.Status(500).JSON(err.Error())
			}

			tmpMap := make(map[int]string)
			concurrentMap.Range(func(key, value interface{}) bool {
				tmpMap[key.(int)] = value.(string)
				return true
			})
			jsonKV, err := json.Marshal(tmpMap)
			_, err2 := fmt.Fprintf(dumpMemoryFile, "%s", jsonKV)
			if err != nil || err2 != nil {
				log.Print(err)
				return c.Status(500).JSON(err.Error())
			}

			for _, v := range vazao {
				_, err := fmt.Fprintf(customLog, "%d\n", v)
				if err != nil {
					log.Print(err)
					return c.Status(500).JSON(err.Error())
				}
			}

			vazao = make([]uint32, 300)
		}

		return c.Status(204).JSON("")
	})

	app.Post("/seed", func(c *fiber.Ctx) error {
		payload := struct {
			Quantity int `json:"quantity"`
			Size     int `json:"size"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON("Error when trying to decode the payload")
		}

		for i := 1; i < payload.Quantity; i++ {
			concurrentMap.Store(i, randSeq(payload.Size))
		}
		return c.Status(204).JSON("")
	})

	app.Delete("/", func(c *fiber.Ctx) error {
		key, err := strconv.Atoi(c.Query("key"))
		if err != nil {
			return c.Status(500).JSON("Não foi possível converter a entrada para numérico")
		}

		concurrentMap.Delete(key)
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
