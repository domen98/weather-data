from flask import Flask, render_template, request, jsonify
import signal
import sys
import json
from publisher import Publisher

app = Flask(__name__)
publisher = Publisher()

@app.route('/')
def home():
    return render_template('index.html', title='Home')

@app.route('/location_weather', methods=['GET'])
def location_weather():
    # Validate latitude and longitude
    try:
        lat = float(request.args.get('lat'))
        lon = float(request.args.get('lon'))
    except (TypeError, ValueError):
        return jsonify({'error': 'Invalid latitude or longitude'}), 400

    # Check if latitude and longitude are within valid ranges
    if not (-90 <= lat <= 90) or not (-180 <= lon <= 180):
        return jsonify({'error': 'Latitude must be between -90 and 90, and longitude between -180 and 180'}), 400

    # Publish latitude and longitude to queue and await response
    try:
        response = publisher.publish_consume(
            'weather-exchange',
            'weather.request',
            json.dumps({
                'latitude': lat,
                'longitude': lon
            })
        )
    except Exception as e:
        return jsonify({'error': str(e)}), 500

    return response, 200
def signal_handler(sig, frame):
    publisher.close_connection()
    sys.exit(0)

if __name__ == '__main__':
    try:
        publisher.create_connection()
    except Exception:
        sys.exit(1)

    signal.signal(signal.SIGINT, signal_handler)  # Handle Ctrl+C
    signal.signal(signal.SIGTERM, signal_handler)  # Handle Docker stop

    # Run the Flask app
    app.run(host='0.0.0.0', port=5000)
