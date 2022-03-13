import datetime

import pandas as pd
import requests
import time
import random
import os

doorflow_auth = requests.auth.HTTPBasicAuth(os.environ['DOORFLOW_KEY'], 'x')
team_map = pd.read_csv('team_group_map.csv', dtype='str')
nexudus_auth = (os.environ['NEXUDUS_USER'],
                os.environ['NEXUDUS_PASS'])

def get_doorflow_user_id(data):

    coworker_email = data['Email']

    url = f"https://admin.doorflow.com/api/2/people?email={coworker_email}"
    doorflow_user = requests.get(url, auth=doorflow_auth).json()

    if len(doorflow_user) == 0:
        print('No Doorflow User with that Email')
        return None

    if str(doorflow_user[0]['system_id'][2:]) != str(data['Id']):
        return 'New User'

    user_doorflow_id = doorflow_user[0]['id']

    return user_doorflow_id


def convert_nexudus_team_to_doorflow_group(data):

    teams = data['Teams']

    groups = []
    for team in teams:

        group = team_map[team_map.nexudus_id == str(team)].doorflow_id
        if not group.empty:
            groups.append(group.iloc[0])

    if "4482" not in groups:
        groups.append("4482")

    return groups

def update_doorflow(coworker_json, deactivate=False):

    print('Updating Doorflow')

    doorflow_user_id = get_doorflow_user_id(coworker_json)

    if doorflow_user_id is None:
        return

    if deactivate:
        groups = []
    else:
        groups = convert_nexudus_team_to_doorflow_group(coworker_json)

    update = {
        "first_name": coworker_json['GuessedFirstName'],
        "last_name": coworker_json["GuessedLastName"],
        "credentials_number": coworker_json['AccessCardId'],
        "key_fob_number": coworker_json['KeyFobNumber'],
        "pin": coworker_json['AccessPincode'],
        "group_ids": groups,
        "enabled": coworker_json['Active'],
        "email": coworker_json['Email'],
        "system_id": 'NX'+str(coworker_json['Id']),
        "notes": "Imported from brown testing"
    }

    if doorflow_user_id == 'New User':
        url = 'https://api.doorflow.com/api/2/people'
        r = requests.post(url, auth=doorflow_auth, json=update)
        return

    url = 'https://api.doorflow.com/api/2/person/{}'.format(doorflow_user_id)
    r = requests.put(url, auth=doorflow_auth, json=update)


def activate(nexudus_coworker_id):

    print(f'Activating door access for {nexudus_coworker_id}')

    url = f"https://spaces.nexudus.com/api/spaces/coworkers/{nexudus_coworker_id}"
    coworker = requests.get(url, auth=nexudus_auth)

    coworker = coworker.json()

    coworker['Active'] = True
    coworker['UserActive'] = True

    update_doorflow(coworker)


def deactivate(nexudus_coworker_id):

    print(f'Deactivating door access for {nexudus_coworker_id}')

    url = f"https://spaces.nexudus.com/api/spaces/coworkers/{nexudus_coworker_id}"
    coworker = requests.get(url, auth=nexudus_auth)

    coworker = coworker.json()

    coworker['Active'] = False
    coworker['UserActive'] = False

    update_doorflow(coworker, deactivate=True)

def update_all_members():

    url = f"https://spaces.nexudus.com/api/spaces/coworkers?page=1&size=1000"

    response = requests.get(url, auth=nexudus_auth)
    coworkers = response.json()['Records']
    random.shuffle(coworkers)

    for coworker in coworkers:

        print('----------')
        coworker_id = coworker['Id']
        active_contracts = coworker['CoworkerContractIds']
        coworker_type = coworker['CoworkerType']

        if active_contracts is None:
            print(coworker['FullName'], 'No Contracts')
            deactivate(coworker_id)
        else:
            print(coworker['FullName'], 'Active')
            activate(coworker_id)

        time.sleep(.5)

    url = 'https://api.doorflow.com/api/2/sync'
    requests.post(url, auth=doorflow_auth)
    print(datetime.datetime.now(), ': Update Doorflow Success')

if __name__ == '__main__':

    update_all_members()
