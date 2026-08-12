package main

import (
	"fmt"
	"log"

	"github.com/atendi9/netio/v2"
	"github.com/atendi9/netio/v2/cors"
)

func main() {
	app, err := netio.New(netio.AppConfig{
		Port: "3333",
		Startup: func(p string) {
			fmt.Printf("Server running on http://localhost:%s\n", p)
			fmt.Println()
			fmt.Println("Test with:")
			fmt.Println()
			fmt.Println(`  # Preflight (OPTIONS):`)
			fmt.Printf("  curl -s -D - -o /dev/null -X OPTIONS -H \"Origin: https://homologaatendi9.netlify.app\" -H \"Access-Control-Request-Method: GET\" -H \"Access-Control-Request-Headers: apikey,authorization\" http://localhost:%s/v1/dashboard/test@test.com/all\n", p)
			fmt.Println()
			fmt.Println(`  # GET with Origin:`)
			fmt.Printf("  curl -s -D - -H \"Origin: https://homologaatendi9.netlify.app\" http://localhost:%s/v1/dashboard/test@test.com/all\n", p)
			fmt.Println()
			fmt.Println(`  # GET with query params:`)
			fmt.Printf("  curl -s -D - -H \"Origin: https://homologaatendi9.netlify.app\" \"http://localhost:%s/v1/dashboard/test@test.com/all?startDate=26/03/2026%%2008:00&endDate=26/03/2026%%2018:00&duration=seconds\"\n", p)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	app.Use(cors.Middleware(cors.Config{
		AllowOrigins:   []string{"https://homologaatendi9.netlify.app"},
		AllowMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowHeaders:   []string{"apikey", "authorization"},
		ExposeHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-Request-ID", "Cache-Control", "Location", "X-Total-Count", "ETag", "X-Powered-By"},
		AllowCredentials: true,
	}))

	app.GET("/v1/dashboard/:gmail/all", func(c *netio.Context) {
		c.JSON(map[string]any{"message": "ok"})
	})

	app.GET("/", func(c *netio.Context) {
		c.JSON(map[string]any{"status": "running"})
	})

	log.Fatal(app.Listen())
}