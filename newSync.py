import time
import datetime
import requests
import urllib
from passwords import secrets
import logging
import pandas as pd

team_map = pd.read_csv('team_group_map.csv', dtype='str')
doorflow_auth = requests.auth.HTTPBasicAuth(
    secrets['doorflow_auth_key'], 'x')
nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])

logging.basicConfig(level=logging.DEBUG)

update_frequency = 1  # in minutes


class coworker():
    def __init__(self, nexudus_record):
        self.raw_record = nexudus_record
        self.nexudus_coworker_id = None
        self.nexudus_user_id = None
        self.nexudus_invoice_status = None
        self.nexudus_due_invoices = []
        self.nexudus_contracts = None
        self.nexudus_teams = []
        self.nexudus_key_fob_number = None
        self.nexudus_credentials_number = None
        self.nexudus_access_pin = None
        self.email_address = None
        self.nexudus_full_name = None
        self.first_name = None
        self.last_name = None
        self.doorflow_user_id = None
        self.doorflow_key_fob_number = None
        self.doorflow_credentials_number = None
        self.doorflow_access_pin = None
        self.doorflow_system_id = None
        self.doorflow_status = None
        self.nexudus_invoice_status = None
        self.updates_successful = False

        self.parse_nexudus_info()

        if self.nexudus_user_id is None:
            logging.info(
                f'{self.nexudus_coworker_id} - {self.first_name} {self.last_name}')
            logging.info(
                f'{self.nexudus_coworker_id} - User is a contact, not a real member')
            logging.info(f'{self.nexudus_coworker_id} - Skipping any updates')
            return

        self.get_doorflow_info()
        self.get_nexudus_invoices()

    def __repr__(self):
        output_string = '\n'
        output_string += f"    Name: {self.nexudus_full_name}\n"
        output_string += f"    ID: {self.nexudus_coworker_id}\n"
        output_string += f"    Contracts: {self.nexudus_contracts}\n"
        output_string += f"    Fob Number: {self.nexudus_key_fob_number}\n"
        output_string += f"    Credentials Number: {self.nexudus_credentials_number}\n"
        output_string += f"    Pin Number: {self.nexudus_access_pin}\n"
        output_string += f"    Invoices Due: {not self.nexudus_invoice_status}\n"
        output_string += f"    Teams: {self.nexudus_teams}\n"
        output_string += f"    DoorFlow Status: {self.doorflow_status}"
        # output_string += f"Contracts: {self.nexudus_contracts}"
        # output_string += f"Contracts: {self.nexudus_contracts}"
        # output_string += f"Contracts: {self.nexudus_contracts}"
        # output_string += f"Contracts: {self.nexudus_contracts}"
        # output_string += f"Contracts: {self.nexudus_contracts}"
        return output_string

    def parse_nexudus_info(self):
        if self.raw_record['KeyFobNumber'] is None:
            self.nexudus_key_fob_number = ''
        else:
            self.nexudus_key_fob_number = self.raw_record['KeyFobNumber']
        if self.raw_record['AccessCardId'] is None:
            self.nexudus_credentials_number = ''
        else:
            self.nexudus_credentials_number = self.raw_record['AccessCardId']
        self.nexudus_access_pin = self.raw_record['AccessPincode']
        self.email_address = self.raw_record['Email']
        self.first_name = self.raw_record['GuessedFirstName']
        self.last_name = self.raw_record['GuessedLastName']

        if self.raw_record['TeamIds'] is not None:
            self.nexudus_teams = self.raw_record['TeamIds'].split(',')
        self.nexudus_contracts = self.raw_record['CoworkerContractIds']
        self.nexudus_coworker_id = self.raw_record['Id']
        self.nexudus_user_id = self.raw_record['UserId']
        self.nexudus_full_name = self.raw_record['FullName']

        if self.first_name is None:
            self.first_name = self.nexudus_full_name.split(' ')[0]

        if self.last_name is None:
            self.last_name = self.nexudus_full_name.split(' ')[-1]

    def get_doorflow_info(self):
        email = urllib.parse.quote_plus(self.email_address, safe='@')
        url = f"https://admin.doorflow.com/api/2/people?email={email}"
        doorflow_user = requests.get(url, auth=doorflow_auth)
        if doorflow_user.status_code == 200:
            logging.debug(
                f'{self.nexudus_coworker_id} - Successfully Found DoorFlow User')
        else:
            logging.debug(
                f'{self.nexudus_coworker_id} - Failed to request doorflow user')
            logging.debug(
                f'{self.nexudus_coworker_id} - {doorflow_user.status_code}')
            logging.debug(
                f'{self.nexudus_coworker_id} - {doorflow_user.reason}')
            return

        try:
            doorflow_user = doorflow_user.json()[0]
        except IndexError:
            logging.info(
                f'{self.nexudus_coworker_id} - Doorflow User Not found with email {self.email_address}')
            return
        self.doorflow_user_id = doorflow_user['id']
        self.doorflow_credentials_number = doorflow_user['credentials_number']
        self.doorflow_key_fob_number = doorflow_user['key_fob_number']
        self.doorflow_access_pin = doorflow_user['pin']
        self.doorflow_groups = [x['id'] for x in doorflow_user['groups']]
        self.doorflow_system_id = doorflow_user['system_id']
        self.doorflow_status = doorflow_user['enabled']

    def create_group_list(self):
        logging.debug(
            f'{self.nexudus_coworker_id} - Creating List of Doorflow Groups')
        group_list = []
        for team in self.nexudus_teams:
            doorflow_team = team_map[team_map.nexudus_id == str(team)]
            if doorflow_team.empty:
                continue
            else:
                logging.debug(
                    f'{self.nexudus_coworker_id} - Adding to team: {doorflow_team.iloc[0].doorflow_name}')
                group_list.append(doorflow_team.iloc[0].doorflow_id)
            logging.debug(
                f'{self.nexudus_coworker_id} - Adding to team: Basic Member')
        group_list.append('4482')
        return group_list

    def get_nexudus_invoices(self):

        logging.debug(
            f'{self.nexudus_coworker_id} - Getting all Unpaid Invoices From Nexudus')
        url = "https://spaces.nexudus.com/api/billing/coworkerinvoices?page=1&size=50&CoworkerInvoice_Paid=false"
        due_invoices = requests.get(url, auth=nexudus_auth)
        if due_invoices.status_code == 200:
            logging.debug(
                f'{self.nexudus_coworker_id} - Successfully Requested Invoices from Nexudus')
        else:
            logging.debug(
                f'{self.nexudus_coworker_id} - Failed Requesting Invoices')
            logging.debug(
                f'{self.nexudus_coworker_id} - {due_invoices.status_code}')
            logging.debug(
                f'{self.nexudus_coworker_id} - {due_invoices.reason}')
            return

        self.nexudus_invoice_status = True

        for invoice in due_invoices.json()['Records']:
            if invoice['CoworkerId'] == self.nexudus_coworker_id:
                self.nexudus_due_invoices.append(invoice)
                self.nexudus_invoice_status = False

    def check_if_update_needed(self):

        if self.doorflow_user_id is None:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Doorflow User Not Created')
            return True

        if self.doorflow_access_pin != self.nexudus_access_pin:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Mismatched Access Pins')
            return True

        if self.doorflow_key_fob_number != self.nexudus_key_fob_number:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Mismatched Key Fob Numbers')
            return True

        if self.doorflow_credentials_number != self.nexudus_credentials_number:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Mismatched Access Card Number')
            return True

        group_match = True
        for team in self.nexudus_teams:
            door_id = team_map[team_map.nexudus_id == str(team)]
            if door_id.empty:
                continue
            else:
                door_id = door_id.iloc[0].doorflow_id
            if int(door_id) not in self.doorflow_groups:
                group_match = False
                break
        if group_match == False:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Door Access does not match')
            return True

        if self.nexudus_contracts is None:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: User Has No Contracts')
            return True

        if self.nexudus_invoice_status == False:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: User has Invoices Due')
            return True

        if self.get_enable_status() != self.doorflow_status:
            logging.info(
                f'{self.nexudus_coworker_id} - Update Needed: Enable Status Does not Match'
            )
            return True

        return False

    def get_enable_status(self):
        logging.debug(
            f'{self.nexudus_coworker_id} - Checking if User should be enabled in Doorflow')
        if self.nexudus_contracts is None:
            logging.debug(f'{self.nexudus_coworker_id} - Enable Status: False')
            logging.debug(f'{self.nexudus_coworker_id} - No Contract')
            return False
        if self.nexudus_invoice_status == False:
            logging.debug(f'{self.nexudus_coworker_id} - Enable Status: False')
            logging.debug(f'{self.nexudus_coworker_id} - Due Invoices')
            return False

        logging.info(
            f'{self.nexudus_coworker_id} - User Should be Enabled in Doorflow')
        return True

    def update_doorflow(self):

        if self.nexudus_coworker_id is None:
            logging.info(
                f"{self.nexudus_coworker_id} - Not a real User in Nexudus. Skipping Updates")
            return

        if not self.check_if_update_needed():
            logging.info(
                f'{self.nexudus_coworker_id} - Doorflow Update Not Needed for {self.nexudus_full_name}')
            return

        if self.doorflow_user_id is None and self.nexudus_contracts is None:
            logging.info(
                f"{self.nexudus_coworker_id} - No Doorflow account and no Contracts")
            logging.info(
                f"{self.nexudus_coworker_id} - Skipping creating user in Doorflow")
            return

        self.create_doorflow_payload()

        if self.doorflow_user_id is None:
            logging.debug(f'{self.nexudus_coworker_id} - Creating New User')
            url = 'https://api.doorflow.com/api/2/people'
            r = requests.post(url, auth=doorflow_auth,
                              json=self.doorflow_payload)
            if r.status_code == 201:
                logging.info(
                    f'{self.nexudus_coworker_id} - {self.nexudus_full_name} created in Doorflow')
                self.updates_successful = True
            else:
                logging.info(
                    f'{self.nexudus_coworker_id} - Failed to create new user in Doorflow')
                logging.info(f'{self.nexudus_coworker_id} - {r.status_code}')
                logging.info(f'{self.nexudus_coworker_id} - {r.text}')
        else:
            logging.debug('Updating User')
            url = 'https://api.doorflow.com/api/2/person/{}'.format(
                self.doorflow_user_id)
            r = requests.put(url, auth=doorflow_auth,
                             json=self.doorflow_payload)
            if r.status_code == 201:
                logging.info(
                    f'{self.nexudus_coworker_id} - {self.first_name} {self.last_name} updated in Doorflow')
                self.updates_successful = True
            else:
                logging.info(
                    f'{self.nexudus_coworker_id} - Failed to update user in Doorflow')
                logging.info(f'{self.nexudus_coworker_id} - {r.status_code}')
                logging.info(f'{self.nexudus_coworker_id} - {r.text}')
                self.updates_successful = False

    def create_doorflow_payload(self):

        logging.info(f'{self.nexudus_coworker_id} - Creating Doorflow Payload')

        groups = self.create_group_list()
        enable_status = self.get_enable_status()

        payload = {
            "first_name": self.first_name,
            "last_name": self.last_name,
            "group_ids": groups,
            "enabled": enable_status,
            "email": self.email_address,
            "system_id": 'NX'+str(self.nexudus_coworker_id),
            "notes": "Updated by syncTools"
        }

        if self.nexudus_credentials_number != None:
            try:
                payload["credentials_number"] = str(
                    self.nexudus_credentials_number)
            except ValueError:
                if len(self.nexudus_credentials_number) == 0:
                    pass
                else:
                    logging.info('Cannot Process Credential Number')

        if self.nexudus_key_fob_number != None:
            try:
                payload["key_fob_number"] = str(
                    self.nexudus_key_fob_number)
            except ValueError:
                if len(self.nexudus_key_fob_number) == 0:
                    pass
                else:
                    logging.info('Cannot Process Key Fob Number')

        if self.nexudus_access_pin != None:
            try:
                payload['pin'] = str(self.nexudus_access_pin)
            except ValueError:
                if len(self.nexudus_access_pin) > 0:
                    pass
                else:
                    logging.info('Cannot Process Pin Number')

        if self.nexudus_contracts is None:
            payload['notes'] += " - No Contract in Nexudus"
        if self.nexudus_invoice_status == False:
            payload['notes'] += " - Invoices Due in Nexudus"

        logging.debug(payload)
        self.doorflow_payload = payload


def get_user_updates():

    start_time = datetime.datetime.now(
        datetime.timezone.utc) - datetime.timedelta(minutes=update_frequency+5)

    logging.info(f'Searching for updates since {start_time}')

    start_string = urllib.parse.quote_plus(
        start_time.strftime("%Y-%m-%dT%H:%M"))
    url = f"https://spaces.nexudus.com/api/spaces/coworkers?from_Coworker_UpdatedOn={start_string}"

    all_updates = []

    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        logging.debug("Good response from user updates request")
        logging.info(f"{len(r.json()['Records'])} User Updates")
        if len(r.json()):
            [all_updates.append(x) for x in r.json()['Records']]
    else:
        logging.info("Failed Request for user updates")
        return False

    url = f"https://spaces.nexudus.com/api/billing/coworkerinvoices?page=1&size=50&from_CoworkerInvoice_PaidOn={start_string}"
    r = requests.get(url, auth=nexudus_auth)
    if r.status_code == 200:
        ids = [x['CoworkerId'] for x in r.json()['Records'] if not x['IsDue']]
        logging.debug("Good response from paid invoice request")
        logging.info(f"{len(ids)} Users Have Paid Invoices")

    if len(ids) > 0:
        for id in ids:
            url = f"https://spaces.nexudus.com/api/spaces/coworkers/{id}"
            r = requests.get(url, auth=nexudus_auth)
            user = r.json()
            all_updates.append(user)

    return {'Records':all_updates}


def push_doorflow_sync():

    url = 'https://api.doorflow.com/api/2/sync'
    requests.post(url, auth=doorflow_auth)
    print('Update Doorflow Success')


def update_users(all_updates):

    updates_sent = False

    for record in all_updates['Records']:

        logging.info("--------------------")

        user = coworker(record)
        logging.info(user)
        user.update_doorflow()
        if user.updates_successful:
            updates_sent = True

    return updates_sent


def main():

    logging.debug("Starting Main Loop")

    now = datetime.datetime.now(datetime.timezone.utc)
    next_run = now - datetime.timedelta(minutes=update_frequency)

    while True:
        now = datetime.datetime.now(datetime.timezone.utc)

        if now < next_run:
            continue

        try:
            updates = get_user_updates()
            if updates:
                updates_sent = update_users(updates)
        except:
            time.sleep(1)
            continue

        if updates_sent:
            logging.info("DoorFlow Updates Sent: Pushing Sync")
            push_doorflow_sync()
        else:
            logging.info("No DoorFlow Updates Sent")
            updates_sent = False

        next_run = now + datetime.timedelta(minutes=update_frequency)


if __name__ == "__main__":
    main()
