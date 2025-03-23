FROM python:3.13-slim

WORKDIR /app

COPY . .

RUN pip install --no-cache-dir -r requirements.txt

RUN chown -R www-data:www-data .

EXPOSE 5000 5678

CMD ["python", "app/main.py"]
