package main

import (
	"github.com/gofiber/fiber/v2"
	"os"
)

func main() {
	app := fiber.New()
	var keyValueDB map[string]string
	keyValueDB = make(map[string]string)

	app.Get("/", func(c *fiber.Ctx) error {
		key := c.Query("key")
		result, ok := keyValueDB[key]
		if ok {
			return c.JSON(result)
		}
		return c.Status(404).JSON("Key not found")
	})

	app.Post("/", func(c *fiber.Ctx) error {
		payload := struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}{}

		if err := c.BodyParser(&payload); err != nil {
			return c.Status(400).JSON("Error when trying to decode the payload")
		}
		keyValueDB[payload.Key] = payload.Value
		return c.Status(204).JSON("")
	})

	app.Delete("/", func(c *fiber.Ctx) error {
		key := c.Query("key")
		delete(keyValueDB, key)

		return c.Status(204).JSON("")
	})

	var portEnv = os.Getenv("PORT")
	if portEnv == "" {
		panic("Couldn't find PORT env")
	}
	err := app.Listen(portEnv)
	if err != nil {
		panic(err)
	}
}
