
import json
import time
from multiprocessing import Process

import pandas as pd
import requests
from flask import Flask, Response, request
from passwords import secrets

team_map = pd.read_csv('team_group_map.csv', dtype='str')
app = Flask(__name__)

doorflow_auth = requests.auth.HTTPBasicAuth(secrets['doorflow_auth_key'], 'x')
nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])


def parse_doorflow_teams(all_doorflow_users, coworker):

    nexudus_id = 'NX'+str(coworker['Id'])
    for user in all_doorflow_users:
        if user['system_id'] == nexudus_id:
            break

    groups = [str(x['id']) for x in user['groups']]

    groups.sort()
    return groups


def get_doorflow_user_id(data):

    coworker_email = data['Email']

    url = f"https://admin.doorflow.com/api/2/people?email={coworker_email}"
    doorflow_user = requests.get(url, auth=doorflow_auth).json()

    if len(doorflow_user) == 0:
        print('No Doorflow User with that Email')
        return 'New Empty User'

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

    groups.sort()
    return groups


def create_new_doorflow_person(request):

    person = {
        "first_name": "New",
        "last_name": "User",
        "pin": "******",
        "enabled": true,
        "mobile": "",
        "notes": "",
        "telephone": "",
        "email": "",
        "valid_from": "2011-11-01",
        "valid_to": "2011-11-25",
        "group_ids": []
    }

    return


def update_doorflow(coworker_json, deactivate=False):

    time.sleep(1)

    print(f"Updating Doorflow for {coworker_json['FullName']}")

    doorflow_user_id = get_doorflow_user_id(coworker_json)

    if doorflow_user_id is None:
        return

    if deactivate:
        groups = []
    else:
        groups = convert_nexudus_team_to_doorflow_group(coworker_json)

    if coworker_json['UserActive'] is None:
        print('Not a real user yet')
        return

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

    if coworker_json['CoworkerContractIds'] is None:
        update['enabled'] = False

    if doorflow_user_id == 'New Empty User':
        print('Creating New User')
        url = 'https://api.doorflow.com/api/2/people'
        update['enabled'] = False
        update['group_ids'] = []
        r = requests.post(url, auth=doorflow_auth, json=update)

    if doorflow_user_id == 'New User':
        url = 'https://api.doorflow.com/api/2/people'
        r = requests.post(url, auth=doorflow_auth, json=update)
        return

    url = 'https://api.doorflow.com/api/2/person/{}'.format(doorflow_user_id)
    r = requests.put(url, auth=doorflow_auth, json=update)

    print(f"Complete: Updating Doorflow for {coworker_json['FullName']}")


def check_invoices(coworker_json):

    nexudus_coworker_id = coworker_json['CoworkerId']
    account_status = True

    print(
        f'Checking if coworker {nexudus_coworker_id} as any outstanding invoices')

    time.sleep(10)

    url = "https://spaces.nexudus.com/api/billing/coworkerinvoices?page=1&size=25&CoworkerInvoice_Paid=false"
    due_invoices = requests.get(url, auth=nexudus_auth)

    for invoice in due_invoices.json()['Records']:
        if invoice['CoworkerId'] == nexudus_coworker_id:
            account_status = False

    if account_status:
        print(f"User {nexudus_coworker_id} has paid all invoices")
        activate(nexudus_coworker_id)
    else:
        print(
            f"Cannot activate account for user {nexudus_coworker_id} because they have outstanding invoices")
        deactivate(nexudus_coworker_id)


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


def update_team_members(team_json):

    nexudus_team_id = str(team_json['Id'])

    doorflow_id = team_map[team_map.nexudus_id == nexudus_team_id]

    if doorflow_id.empty:
        print('Team not associated with a shop')
        return

    url = "https://admin.doorflow.com/api/2/people?per_page=1000"
    all_doorflow_users = requests.get(url, auth=doorflow_auth).json()

    doorflow_id = doorflow_id.iloc[0].doorflow_id

    for nexudus_coworker_id in team_json['TeamMembers']:
        url = f"https://spaces.nexudus.com/api/spaces/coworkers/{nexudus_coworker_id}"
        coworker = requests.get(url, auth=nexudus_auth)
        coworker = coworker.json()

        new_doorflow_teams = convert_nexudus_team_to_doorflow_group(coworker)
        old_doorflow_teams = parse_doorflow_teams(all_doorflow_users, coworker)

        if new_doorflow_teams != old_doorflow_teams:
            print(f'Adding {nexudus_coworker_id} to team {doorflow_id}')
            update_doorflow(coworker)

    for doorflow_user in all_doorflow_users:
        current_doorflow_groups = [x['id'] for x in doorflow_user['groups']]
        if int(doorflow_id) in current_doorflow_groups:
            nexudus_coworker_id = int(doorflow_user['system_id'][2:])
            if nexudus_coworker_id not in team_json['TeamMembers']:
                print(
                    f'Removing {nexudus_coworker_id} from team {doorflow_id}')
                url = f"https://spaces.nexudus.com/api/spaces/coworkers/{nexudus_coworker_id}"
                coworker = requests.get(url, auth=nexudus_auth)
                coworker = coworker.json()
                try:
                    update_doorflow(coworker)
                except:
                    if nexudus_coworker_id == 1414851185:
                        pass
                    else:
                        print(f'Something went wrong with user {nexudus_coworker_id}')

@app.route('/AccessControlUpdate', methods=['POST'])
def update_access_control():

    coworker = request.json[0]

    update = Process(
        target=update_doorflow,
        daemon=True,
        args=(coworker,)
    )
    update.start()
    return Response(status=200)


@app.route('/InvoicePaid', methods=['POST'])
def invoice_paid():

    coworker = request.json[0]

    update = Process(
        target=check_invoices,
        daemon=True,
        args=(coworker,)
    )
    update.start()
    return Response(status=200)


@app.route('/InvoiceIssued', methods=['POST'])
def invoice_issued():

    coworker = request.json[0]

    update = Process(
        target=check_invoices,
        daemon=True,
        args=(coworker,)
    )
    update.start()
    return Response(status=200)


@app.route('/MembershipExpired', methods=['POST'])
def expired_membership():

    coworker = request.json[0]
    coworker_id = coworker['CoworkerId']

    update = Process(
        target=deactivate,
        daemon=True,
        args=(coworker_id,)
    )
    update.start()
    return Response(status=200)


@app.route('/TeamUpdate', methods=['POST'])
def team_update():

    team = request.json[0]
    update = Process(
        target=update_team_members,
        daemon=True,
        args=(team,)
    )
    update.start()
    return Response(status=200)


@app.route('/Checkin', methods=['POST'])
def checkin_user():

    return Response(status=200)


if __name__ == '__main__':
    app.run(debug=True, port=5500)
