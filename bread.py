from locust import HttpUser, task, between
import random
import string

# Generate a random ASCII key-value pair
def random_ascii_string(length=256):
    return ''.join(random.choices(string.ascii_letters + string.digits + string.punctuation, k=length))

class KeyValueStoreTest(HttpUser):
    wait_time = between(1, 3)  # Simulate real-world wait times between requests
    keys = []  # Store keys that were successfully inserted

    @task(2)  # PUT task runs twice as often as GET
    def set_value(self):
        """ Inserts a random key-value pair into the key-value store """
        key = random_ascii_string(random.randint(1, 256))
        value = random_ascii_string(random.randint(1, 256))

        response = self.client.post("/put", json={"key": key, "value": value})
        if response.status_code == 200:
            self.keys.append(key)  # Store successfully inserted keys for GET

    @task(1)
    def get_value(self):
        """ Fetches a value for a previously inserted key """
        if self.keys:  # Ensure we only request existing keys
            key = random.choice(self.keys)
            self.client.get(f"/get?key={key}")