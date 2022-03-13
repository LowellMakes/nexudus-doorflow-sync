import requests
import datetime
import time
import os

doorflow_auth = requests.auth.HTTPBasicAuth(os.environ['DOORFLOW_KEY'], 'x')

while True:

    url = 'https://api.doorflow.com/api/2/sync'
    requests.post(url, auth=doorflow_auth)
    print(datetime.datetime.now(), ': Update Doorflow Success')

    time.sleep(600)
