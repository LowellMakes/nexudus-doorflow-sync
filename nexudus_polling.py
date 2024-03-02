import time
import datetime
import requests
import urllib
from passwords import secrets
import logging
import pandas as pd
import sqlite3


def dict_factory(cursor, row):
    fields = [column[0] for column in cursor.description]
    return {key: value for key, value in zip(fields, row)}


team_map = pd.read_csv('team_group_map.csv', dtype='str')
doorflow_auth = requests.auth.HTTPBasicAuth(
    secrets['doorflow_auth_key'], 'x')
nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])
con = sqlite3.connect("nexudus_database.db")
con.row_factory = dict_factory

logging.basicConfig(level=logging.DEBUG)

update_frequency = 5  # in minutes


def format_datetime(input):

    output = int(pd.to_datetime(input).timestamp())

    return output


def get_team_updates():
    teams = team_map['nexudus_id'].to_list()
    ids_to_update = []
    for team in teams:
        if team == 'None':
            continue
        url = f"https://spaces.nexudus.com/api/spaces/teams/{team}"
        r = requests.get(url, auth=nexudus_auth)
        nex_members = r.json()['TeamMembers']
        cur = con.execute(
            f"SELECT id from nexudus_members WHERE TeamIds like '%{team}%'")
        local_members = [int(x['Id']) for x in cur.fetchall()]
        diff = set(nex_members).symmetric_difference(set(local_members))
        if len(diff):
            logging.debug(f"Updates Needed on Team {team}")
            for x in diff:
                logging.debug(x)
        [ids_to_update.append(x) for x in diff]

    ids_to_update = list(set(ids_to_update))

    all_updates = []

    for id in ids_to_update:
        url = f"https://spaces.nexudus.com/api/spaces/coworkers/{id}"
        r = requests.get(url, auth=nexudus_auth)
        user = r.json()
        all_updates.append(user)
        logging.info(f"From Team Update - Adding User: {user['FullName']}")

    return all_updates


def get_full_nexudus_database():

    logging.info('Getting complete record of Nexudus Database')

    url = f"https://spaces.nexudus.com/api/spaces/coworkers?size=100000"
    r = requests.get(url, auth=nexudus_auth)

    if r.status_code == 200:
        logging.debug("Good response for complete nexudus database request")
    else:
        logging.fatal('COULD NOT GET ALL COWORKER RECORDS FROM NEXUDUS')
        logging.fatal(r.text)

    r = r.json()
    data = pd.DataFrame(r['Records'])
    data = data.drop(columns=['CustomFields'])
    data = data.astype(str)
    data['UpdatedOn'] = [format_datetime(x) for x in data['UpdatedOn']]
    data['NeedUpdate'] = False
    data.to_sql(name='nexudus_members', con=con, if_exists='replace')


def get_user_updates():

    start_time = datetime.datetime.now(
        datetime.timezone.utc) - datetime.timedelta(minutes=update_frequency+1)
    logging.info(f'Searching for updates since {start_time}')

    start_string = urllib.parse.quote_plus(
        start_time.strftime("%Y-%m-%dT%H:%M"))

    all_ids = []

    # Check for nexudus flagged updates
    url = f"https://spaces.nexudus.com/api/spaces/coworkers?from_Coworker_UpdatedOn={start_string}"
    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        for user in r.json()['Records']:
            logging.info(
                f"From Nexudus Updates - Adding User: {user['FullName']}")
            all_ids.append(user['Id'])

    url = "https://spaces.nexudus.com/api/billing/coworkerinvoices?CoworkerInvoice_Paid=false&CoworkerInvoice_Void=false"
    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        for user in r.json()['Records']:
            logging.info(
                f"From Due Invoices - Adding User: {user['CoworkerFullName']}")
            all_ids.append(user['CoworkerId'])

    all_ids = list(set(all_ids))

    all_updates = []

    if len(all_ids) > 0:
        for id in all_ids:
            url = f"https://spaces.nexudus.com/api/spaces/coworkers/{id}"
            r = requests.get(url, auth=nexudus_auth)
            user = r.json()
            all_updates.append(user)

    return all_updates


def update_users(updates):

    for update in updates:
        logging.info(f"    Updating user: {update['FullName']}")
        logging.debug(f"    User ID: {update['Id']}")
        cur = con.execute(
            f"SELECT * from nexudus_members WHERE id={update['Id']}")
        local = cur.fetchone()
        update_flag = False
        for key in local.keys():
            if key in ['index', 'UpdatedOn', 'NeedUpdate']:
                continue
            if str(local[key]) != str(update[key]):
                update_flag = True
                con.execute(
                    f"UPDATE nexudus_members SET '{key}'='{update[key]}' WHERE id={update['Id']}")
                logging.debug(
                    f"    Updating {key} from {local[key]} to {update[key]}")
                con.execute(
                    f"UPDATE nexudus_members SET 'NeedUpdate'= True WHERE id={update['Id']}")
        if update_flag:
            con.execute(
                f"UPDATE nexudus_members SET 'UpdatedOn'='{format_datetime(datetime.datetime.now(datetime.timezone.utc))}' WHERE id={update['Id']}")
        con.commit()


def create_groups(team_string):

    group_list = []
    group_list.append('4482')  # Group ID for Basic Membership

    if team_string == "None":
        return group_list

    teams = team_string.split(',')
    for team in teams:
        doorflow_team = team_map[team_map.nexudus_id == str(team)]
        if doorflow_team.empty:
            continue
        else:
            group_list.append(doorflow_team.iloc[0].doorflow_id)

    return group_list


def get_enable_status(user):

    if user['CoworkerContractIds'] == 'None':
        return False

    url = f"https://spaces.nexudus.com/api/billing/coworkerinvoices?CoworkerInvoice_Coworker={user['Id']}&CoworkerInvoice_Paid=false&CoworkerInvoice_Void=false"
    r = requests.get(url, auth=nexudus_auth)
    r.json()['Records']

    if len(r.json()['Records']):
        return False

    return True


def push_doorflow_changes():

    logging.info("Checking for Doorflow Updates")

    cur = con.execute(
        f"SELECT * from nexudus_members WHERE NeedUpdate=True")
    local = cur.fetchall()

    need_sync = False

    if not local:
        logging.info("No Doorflow updates needed")

    for update_user in local:
        logging.debug(
            f"Updating: {update_user['GuessedFirstName']} {update_user['GuessedLastName']}")
        logging.debug(f"ID: {update_user['Id']}")
        payload = {
            "first_name": update_user['GuessedFirstName'],
            "last_name": update_user['GuessedLastName'],
            "group_ids": create_groups(update_user['TeamIds']),
            "enable": get_enable_status(update_user),
            "email": update_user['Email'],
            "system_id": 'NX'+str(update_user['Id']),
            "notes": "Updates by syncTools V3",
        }

        if update_user['AccessCardId'] not in ['None', '']:
            payload["credentials_number"] = str(
                int(update_user['AccessCardId']))

        if update_user['AccessPincode'] not in ['None', '']:
            payload["pin"] = str(int(update_user['AccessPincode']))

        if update_user['KeyFobNumber'] not in ['None', '']:
            payload["key_fob_number"] = str(int(update_user['KeyFobNumber']))

        logging.debug(payload)

        url = 'https://api.doorflow.com/api/2/people'
        r = requests.post(url, auth=doorflow_auth,
                          json=payload)
        if r.status_code == 422:
            logging.debug("Cannot create new user")
            email = urllib.parse.quote_plus(update_user['Email'], safe='@')
            url = f"https://admin.doorflow.com/api/2/people?email={email}"
            doorflow_user = requests.get(url, auth=doorflow_auth)
            doorflow_id = doorflow_user.json()[0]['id']
            url = 'https://api.doorflow.com/api/2/person/{}'.format(
                doorflow_id)
            r = requests.put(url, auth=doorflow_auth,
                             json=payload)
            if r.status_code == 201:
                logging.debug("User Updated")
            else:
                logging.debug("Something went wrong with the doorflow update")
        else:
            logging.debug("New User Created")

        con.execute(
            f"UPDATE nexudus_members SET 'NeedUpdate'= 'False' WHERE id={update_user['Id']}")
        con.commit()
        need_sync = True

    return need_sync


def push_doorflow_sync():

    logging.info('Pushing Doorflow Sync')
    url = 'https://api.doorflow.com/api/2/sync'
    requests.post(url, auth=doorflow_auth)


def main():

    logging.debug("Starting Main Loop")

    get_full_nexudus_database()

    now = datetime.datetime.now(datetime.timezone.utc)
    next_run = now - datetime.timedelta(minutes=update_frequency)

    while True:
        now = datetime.datetime.now(datetime.timezone.utc)
        if now < next_run:
            continue

        updates = get_user_updates()
        updates += get_team_updates()
        if updates:
            update_users(updates)
            need_sync = push_doorflow_changes()
            if need_sync:
                push_doorflow_sync()

        next_run = now + datetime.timedelta(minutes=update_frequency)


if __name__ == "__main__":

    main()
