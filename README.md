# Weather Location Data App

A simple weather location data app with a web interface that fetches current weather data from the OpenWeatherMap API.

It consists of:
- Python Flask web app that servers the UI and publishes request to queue,
- Gp app consuming messages, making requests to OpenWeatherMap API and responding back to queue,
- RabbitMQ broker for event driven arch
- Reddis DB for caching API responses

## Setup Instructions

### Prerequisites
- Ensure you have **Docker** and **docker-compose** installed locally.

### Setup Steps
1. **Clone the Repository**
   ```sh
   git clone git@github.com:domen98/weather-data.git
   cd weather-data
   ```

2. **Set Up Environment Variables**
   - Copy the example environment file:
     ```sh
     cp .env.example .env
     ```
   - In `.env` file set `OPEN_WEATHER_API_KEY` variable to your OpenWeatherMap app ID
   - (Optional) Modify `.env` to set custom application port numbers.

3. **Configure Hosts File**
   - Add the following lines to your `/etc/hosts` file:
     ```sh
     127.0.0.1 redis
     127.0.0.1 rabbitmq
     ```

4. **Run the Application**
   ```sh
   docker-compose up
   ```

## Usage
- The UI can be accessed at `localhost:8080` (or any port value set in `FLASK_PORT`).
- Consumer metrics can be accessed at `localhost:5000/metrics` (or any port value set in `CONSUMER_PORT`).
- RabbitMQ dashboard can be accessed at `localhost:15672`.
- Redis can be accessed from local machine (install tool `redis-cli`):
  ```sh
  redis-cli -h redis
  ```

## License
This project is licensed under the MIT License.
