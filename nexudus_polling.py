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
        if pd.isna(team):
            continue
        url = f"https://spaces.nexudus.com/api/spaces/teams/{team}"
        r = requests.get(url, auth=nexudus_auth)
        nex_members = r.json()['TeamMembers']
        cur = con.execute(f"SELECT id from nexudus_members WHERE TeamIds like '%{team}%'")
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
    data['UpdatedOn'] = pd.to_datetime(data['UpdatedOn'])
    data.to_sql(name='nexudus_members', con=con, if_exists='replace')

def get_user_updates():

    start_time = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=update_frequency+1)
    logging.info(f'Searching for updates since {start_time}')

    start_string = urllib.parse.quote_plus(
        start_time.strftime("%Y-%m-%dT%H:%M"))

    all_ids = []

    # Check for nexudus flagged updates
    url = f"https://spaces.nexudus.com/api/spaces/coworkers?from_Coworker_UpdatedOn={start_string}"
    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        for user in r.json()['Records']:
            logging.info(f"From Nexudus Updates - Adding User: {user['FullName']}")
            all_ids.append(user['Id'])

    url = "https://spaces.nexudus.com/api/billing/coworkerinvoices?CoworkerInvoice_Paid=false&CoworkerInvoice_Void=false"
    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        for user in r.json()['Records']:
            logging.info(f"From Due Invoices - Adding User: {user['CoworkerFullName']}")
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
        cur = con.execute(f"SELECT * from nexudus_members WHERE id={update['Id']}")
        local = cur.fetchone()
        update_flag = False
        for key in local.keys():
            if key in ['index','UpdatedOn']:
                continue
            if str(local[key]) != str(update[key]):
                update_flag = True
                con.execute(f"UPDATE nexudus_members SET '{key}'='{update[key]}' WHERE id={update['Id']}")
                logging.debug(f"    Updating {key} from {local[key]} to {update[key]}")
        if update_flag:
            con.execute(f"UPDATE nexudus_members SET 'UpdatedOn'='{format_datetime(datetime.datetime.now(datetime.timezone.utc))}' WHERE id={update['Id']}")
        con.commit()

def main():

    logging.debug("Starting Main Loop")

    get_full_nexudus_database()

    now = datetime.datetime.now(datetime.timezone.utc)
    next_run = now - datetime.timedelta(minutes=update_frequency)

    while True:
        now = datetime.datetime.now(datetime.timezone.utc)
        if now < next_run:
            continue

        # try:
        updates = get_user_updates()
        updates += get_team_updates()
        if updates:
            update_users(updates)
        # except:
        #     time.sleep(1)
        #     continue

        next_run = now + datetime.timedelta(minutes=update_frequency)

if __name__ == "__main__":
    # get_full_nexudus_database()
    main()
