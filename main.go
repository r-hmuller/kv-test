package main

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var isBeingTested atomic.Bool

type sample struct {
	timestamp  int64
	throughput uint32
}

var vazao []sample
var vazaoMu sync.Mutex

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
			if isBeingTested.Load() {
				t := atomic.SwapUint32(&ex.thrCount, 0)
				vazaoMu.Lock()
				vazao = append(vazao, sample{timestamp: time.Now().Unix(), throughput: t})
				vazaoMu.Unlock()
			}
		}
	}
}

type serverController struct {
	app      *fiber.App
	listener net.Listener
	port     string
}

// startListener inicia um novo listener de rede e o associa ao Fiber.
// Roda em uma goroutine para não bloquear.
func (sc *serverController) startListener() {
	ln, err := net.Listen("tcp", sc.port)
	if err != nil {
		return
	}
	sc.listener = ln

	go func() {
		sc.app.Listener(ln)
	}()
}

// stopListener fecha o listener atual para pausar o recebimento de novas conexões.
func (sc *serverController) stopListener() {
	if sc.listener == nil {
		return
	}
	sc.listener.Close()
	sc.listener = nil
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func logErrors(route string, h fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := h(c)
		if status := c.Response().StatusCode(); status >= 400 {
			log.Printf("%s [%d]: %v", route, status, err)
		}
		return err
	}
}

var concurrentMap sync.Map

func main() {
	isBeingTested.Store(false)
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

	app.Post("/testing", logErrors("/testing", func(c *fiber.Ctx) error {
		payload := struct {
			Action string `json:"action"`
			Path   string `json:"path"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON("Error when trying to decode the payload")
		}
		if payload.Action == "stop" && payload.Path == "" {
			return c.Status(400).JSON("path is required when action is stop")
		}
		if payload.Action == "start" {
			isBeingTested.Store(true)
		}
		if payload.Action == "stop" {
			isBeingTested.Store(false)
			flags := os.O_CREATE | os.O_TRUNC | os.O_WRONLY | os.O_APPEND | os.O_SYNC
			customLog, err := os.OpenFile(payload.Path, flags, 0600)
			if err != nil {
				return c.Status(500).JSON(err.Error())
			}
			defer customLog.Close()

			vazaoMu.Lock()
			snapshot := vazao
			vazao = make([]sample, 0)
			vazaoMu.Unlock()

			for _, v := range snapshot {
				_, err := fmt.Fprintf(customLog, "%d,%d\n", v.timestamp, v.throughput)
				if err != nil {
					return c.Status(500).JSON(err.Error())
				}
			}
		}

		return c.Status(204).JSON("")
	}))

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

	port := os.Getenv("PORT")
	if port == "" {
		panic("Couldn't find PORT env")
	}
	if port[0] != ':' {
		port = ":" + port
	}

	// Cria nosso controlador de servidor.
	sc := &serverController{
		app:  app,
		port: port,
	}

	// Inicia o servidor pela primeira vez.
	sc.startListener()

	// --- Gerenciamento de Sinais ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)

	for {
		sig := <-sigChan
		switch sig {
		case syscall.SIGUSR1:
			sc.stopListener()

		case syscall.SIGUSR2:
			if sc.listener != nil {
				continue
			}
			sc.startListener()

		case syscall.SIGINT, syscall.SIGTERM:
			_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			app.Shutdown()
			ex.logFile.Close()
			return
		}
	}
}
