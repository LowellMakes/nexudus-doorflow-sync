import newSync
import requests
import time
from passwords import secrets

nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])


def update_all_users():

    url = f"https://spaces.nexudus.com/api/spaces/coworkers?size=10000"
    r = requests.get(url, auth=nexudus_auth)
    newSync.update_users(r.json())

    newSync.push_doorflow_sync()


if __name__ == "__main__":

    update_all_users()
