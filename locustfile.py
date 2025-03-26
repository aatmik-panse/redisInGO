import json
import random
import string
from locust import HttpUser, task, between

class CacheUser(HttpUser):
    # Define the host here to avoid the error
    host = "http://localhost:7171"
    wait_time = between(0.5, 2)
    
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.keys = []
    
    def random_string(self, length=10):
        return ''.join(random.choice(string.ascii_letters) for _ in range(length))
    
    @task(2)
    def put_cache(self):
        key = self.random_string()
        value = self.random_string(50)
        self.keys.append(key)
        
        # Limit the keys list to prevent excessive memory usage
        if len(self.keys) > 100:
            self.keys = self.keys[-100:]
            
        payload = {
            "key": key,
            "value": value
        }
        
        response = self.client.post("/put", json=payload)
        if response.status_code != 200:
            print(f"Failed to put cache: {response.text}")

    @task(8)
    def get_cache(self):
        if not self.keys:
            # If we don't have any keys yet, put something in cache first
            self.put_cache()
            return
            
        # Randomly select a key that we've previously stored
        key = random.choice(self.keys)
        response = self.client.get(f"/get?key={key}")
        if response.status_code != 200:
            print(f"Failed to get cache: {response.text}")

