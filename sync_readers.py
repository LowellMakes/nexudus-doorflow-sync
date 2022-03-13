import requests
import datetime
import time

doorflow_auth = requests.auth.HTTPBasicAuth('3zxv-BQUSiSyER5UxrM4', 'x')

while True:

    url = 'https://api.doorflow.com/api/2/sync'
    requests.post(url, auth=doorflow_auth)
    print(datetime.datetime.now(), ': Update Doorflow Success')

    time.sleep(600)
