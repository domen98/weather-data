package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/go-resty/resty/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	redis "github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v4/cpu"
)

var (
	serverStart       time.Time
	processedMessages int
	errorsCount       int
	mu                sync.Mutex
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Start RabbitMQ consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		startRabbitMQConsumer(ctx)
	}()

	// Start HTTP server for metrics
	wg.Add(1)
	go func() {
		defer wg.Done()
		startHttpServer()
	}()

	// Wait for one of the shutdown signals to occur and triggers context cancel
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutdown started...")
	cancel()
	wg.Wait()
}

func startRabbitMQConsumer(ctx context.Context) {
	rdb := createRedisClient(&ctx, 0)
	defer rdb.Close()

	conn := createRabbitmqConnection(0)
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// Consume messages from the predefined queue
	msgs, err := ch.Consume(
		"weather-requests", // queue name
		"",                 // consumer
		false,              // auto-ack
		false,              // exclusive
		false,              // no-local
		false,              // no-wait
		nil,                // args
	)

	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	log.Println("RabbitMQ consumer started...")

	// Process messages
	for {
		select {
		case msg := <-msgs:
			go func(message amqp.Delivery) {
				defer func() {
					panic := recover()

					if panic != nil {
						log.Printf("Recovered from panic: %v", panic)
						incrementMetric(&errorsCount)
					}
				}()

				var response json.RawMessage

				reply := func() {
					// Publish the response to the reply_to queue if correlation_id and reply_to are provided
					if message.CorrelationId != "" && message.ReplyTo != "" {
						err = ch.Publish(
							"",
							message.ReplyTo,
							false,
							false,
							amqp.Publishing{
								ContentType:   "application/json",
								CorrelationId: message.CorrelationId,
								Body:          response,
							},
						)

						// TODO: implement dead letter exchange failed replies
						if err != nil {
							log.Printf("Failed to publish response: %v", err)
							incrementMetric(&errorsCount)
						}
					}

					// Acknowledge the original message to mark it as sucesfully processed
					if err := message.Ack(false); err != nil {
						log.Printf("Failed to acknowledge message: %v", err)
						incrementMetric(&errorsCount)
					}

					incrementMetric(&processedMessages)
				}

				// Validate message and extract lat and lon
				latitude, longitude, err := parseMessageBody(message)

				if err != nil {
					response = buildErrorMessageResponse(err)
					reply()

					return
				}

				// Call external API
				response, err = processMessage(latitude, longitude, rdb, &ctx)

				if err != nil {
					response = buildErrorMessageResponse(err)
				}

				reply()
			}(msg)
		case <-ctx.Done():
			log.Println("Stopping RabbitMQ consumer...")
			return
		}
	}
}

func startHttpServer() {
	log.Println("Starting HTTP server on :5000...")
	serverStart = time.Now()

	http.HandleFunc("/metrics", metricsHandler)
	err := http.ListenAndServe(":5000", nil)

	if err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}
}

func createRabbitmqConnection(retries int) *amqp.Connection {
	conn, err := amqp.Dial(fmt.Sprintf(
		"amqp://%s:%s@%s:5672/",
		getEnvVariable("RABBITMQ_DEFAULT_USER", "guest"),
		getEnvVariable("RABBITMQ_DEFAULT_PASS", "guest"),
		getEnvVariable("RABBITMQ_HOST", "localhost"),
	))

	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	if err != nil {
		if retries < 5 {
			retries++
			log.Printf("failed to connect to RabbitMQ. Retry no %d", retries)
			time.Sleep(1 * time.Second)

			return createRabbitmqConnection(retries)
		}

		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	log.Println("Connected to RabbitMQ")

	return conn
}

func createRedisClient(ctx *context.Context, retries int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:6379", getEnvVariable("REDIS_HOST", "localhost")),
		Password: "",
		DB:       0,
	})

	pong, err := rdb.Ping(*ctx).Result()

	if err != nil {
		if retries < 5 {
			retries++
			log.Printf("failed to connect to Redis. Retry no %d", retries)
			time.Sleep(1 * time.Second)

			return createRedisClient(ctx, retries)
		}

		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	log.Println("Connected to Redis:", pong)

	return rdb
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	var uptime time.Duration = time.Since(serverStart)
	hours := int(uptime.Hours())
	minutes := int(uptime.Minutes()) % 60
	seconds := int(uptime.Seconds()) % 60

	cpuPercentages, _ := cpu.Percent(time.Second, false)

	var allocMemory runtime.MemStats
	runtime.ReadMemStats(&allocMemory)

	metrics := map[string]any{
		"processed_messages": processedMessages,
		"errors_count":       errorsCount,
		"memory_usage":       fmt.Sprintf("%.2f KB", float64(allocMemory.Alloc/1024)),
		"cpu_usage":          fmt.Sprintf("%.2f%%", cpuPercentages[0]),
		"uptime":             fmt.Sprintf("%dh:%dm:%ds", hours, minutes, seconds),
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		http.Error(w, "Failed to encode metrics", http.StatusInternalServerError)
	}
}

func parseMessageBody(message amqp.Delivery) (float64, float64, error) {
	if message.ContentType != "application/json" {
		return 0, 0, fmt.Errorf("message content_type needs to be application/json")
	}

	var data map[string]float64
	err := json.Unmarshal(message.Body, &data)

	if err != nil {
		return 0, 0, fmt.Errorf("failed to unmarshal JSON")
	}

	// Extract latitude and longitude from the map
	latitude, ok := data["latitude"]

	if !ok {
		return 0, 0, fmt.Errorf("invalid or missing parameter: latitude")
	}

	longitude, ok := data["longitude"]

	if !ok {
		return 0, 0, fmt.Errorf("invalid or missing parameter longitude")
	}

	return latitude, longitude, nil
}

func processMessage(latitude, longitude float64, rdb *redis.Client, ctx *context.Context) (json.RawMessage, error) {
	var response json.RawMessage
	var hash [16]byte = md5.Sum(fmt.Appendf(nil, "%f|%f", latitude, longitude))
	key := fmt.Sprintf("location_weather:%s", hex.EncodeToString(hash[:]))

	// Check if location weather data is already store id Redis before making call to external API
	value, err := rdb.Get(*ctx, key).Result()

	if err != nil {
		response, err = callExternalAPI(latitude, longitude)

		if err != nil {
			return nil, err
		}

		err = rdb.Set(*ctx, key, []byte(response), time.Hour).Err()

		if err != nil {
			log.Println(fmt.Errorf("error setting value: %v", err))
		}
	} else {
		response = json.RawMessage(value)
	}

	return response, err
}

func callExternalAPI(latitude, longitude float64) (json.RawMessage, error) {
	client := resty.New()
	client.SetTimeout(15 * time.Second)

	log.Println("Calling external API...")

	// Call OpenWeatherMap API
	response, err := client.R().
		SetQueryParams(map[string]string{
			"lat":     fmt.Sprintf("%f", latitude),
			"lon":     fmt.Sprintf("%f", longitude),
			"exclude": "minutely,hourly,daily",
			"appid":   getEnvVariable("OPEN_WEATHER_API_KEY", ""),
		}).
		Get("https://api.openweathermap.org/data/3.0/onecall")

	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %s", response.Status())
	}

	var jsonResponse json.RawMessage

	if json.Unmarshal(response.Body(), &jsonResponse) != nil {
		return nil, fmt.Errorf("response is not a valid JSON: %v", err)
	}

	return jsonResponse, nil
}

func buildErrorMessageResponse(err error) json.RawMessage {
	log.Println("Missing correlation_id or reply_to message properties")
	incrementMetric(&errorsCount)

	return json.RawMessage(fmt.Sprintf(`{"message": "%v"}`, err))
}

func incrementMetric(ptr *int) {
	mu.Lock()
	*ptr++
	mu.Unlock()
}

func getEnvVariable(name string, defaultValue string) string {
	value, isSet := os.LookupEnv(name)

	if !isSet && defaultValue == "" {
		log.Fatalf("Environment variable %s is not defined!", name)
	}

	if value != "" {
		return value
	}

	return defaultValue
}
