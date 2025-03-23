import pika
import pika.credentials
from pika.exceptions import StreamLostError
import logging
import os
import time
import uuid

class Publisher:
    __connection = None
    __logger = None

    def __init__(self):
        self.__logger = logging.getLogger(__name__)
        self.__logger.setLevel(logging.DEBUG)
        handler = logging.StreamHandler()
        formatter = logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s')
        handler.setFormatter(formatter)
        self.__logger.addHandler(handler)

    def create_connection(self, retries: int = 0):
        try:
            self.__connection = pika.BlockingConnection(
                pika.ConnectionParameters(
                    host=os.getenv('RABBITMQ_HOST', 'localhost'),
                    heartbeat=300,
                    credentials = pika.PlainCredentials(
                        os.getenv('RABBITMQ_DEFAULT_USER', 'guest'), 
                        os.getenv('RABBITMQ_DEFAULT_PASS', 'guest')
                    )
                )
            )

            self.__logger.debug("RabbitMQ connection established.")
        except Exception:
            self.__logger.debug(f"Failed to establish RabbitMQ connection. Retry no. {retries}")

            if retries >= 5:
                self.__logger.debug(f"Failed to establish RabbitMQ connection for the final time")
                raise RuntimeError("RabbitMQ connection is not established.")

            time.sleep(1)
            self.create_connection(retries + 1)

    def close_connection(self) -> None:
        if self.__connection and self.__connection.is_open:
            self.__connection.close()
            self.__logger.debug("RabbitMQ connection closed.")

    def publish_consume(self, exchange: str, routing_key: str, message: str, retry: bool = True) -> str:
        channel = None

        try:
            channel = self.__connection.channel()
            channel.confirm_delivery()

            correlation_id = str(uuid.uuid4())
            response: str|None = None

            # Callback for handling the response
            def on_response(ch, method, props, body):
                nonlocal response
                if correlation_id == props.correlation_id:
                    if props.content_type == 'application/json':
                        response = body.decode('utf-8')
                    else:
                        raise Exception(f"Expected response is not json. Response type {props.content_type}")

            # Declare a temporary reply queue
            result = channel.queue_declare(
                '', 
                exclusive=True, 
                auto_delete=True
            )
            callback_queue = result.method.queue

            # Set up consumer for the reply queue
            channel.basic_consume(
                queue=callback_queue,
                on_message_callback=on_response,
                auto_ack=True
            )

            # Publish the message
            channel.basic_publish(
                exchange=exchange,
                routing_key=routing_key,
                body=message,
                properties=pika.BasicProperties(
                    delivery_mode=2,  # Persistent message
                    content_type='application/json',
                    reply_to=callback_queue,
                    correlation_id=correlation_id
                ),
                mandatory=True
            )
            self.__logger.debug("Message published, waiting for response...")

            # Wait for the response
            start_time = time.time()
            while response is None:
                if time.time() - start_time > 30.0:
                    raise TimeoutError("Timeout: No response received within the specified time.")

                self.__connection.process_data_events()

            return response
        except Exception as e:
            if retry and isinstance(e, (ConnectionError, ConnectionResetError, StreamLostError)):
                self.create_connection()

                return self.publish_consume(exchange, routing_key, message, False)

            self.__logger.error(f"Failed to publish message: {e}")
            raise e
        finally:
            if channel and channel.is_open:
                channel.close()
                