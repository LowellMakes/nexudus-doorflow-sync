from multiprocessing.sharedctypes import Value
import requests
from passwords import secrets
import logging
import pandas as pd
import time
import urllib

team_map = pd.read_csv('team_group_map.csv', dtype='str')
doorflow_auth = requests.auth.HTTPBasicAuth(
    secrets['doorflow_auth_key'], 'x')
nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])
logging.basicConfig(level=logging.DEBUG)


class coworker():
    def __init__(self, nexudus_coworker_id):
        self.nexudus_coworker_id = nexudus_coworker_id
        self.nexudus_invoice_status = None
        self.nexudus_due_invoices = []
        self.nexudus_contracts = None
        self.nexudus_teams = []
        self.nexudus_key_fob_number = None
        self.nexudus_credentials_number = None
        self.nexudus_access_pin = None
        self.email_address = None
        self.first_name = None
        self.last_name = None
        self.doorflow_user_id = None
        self.doorflow_key_fob_number = None
        self.doorflow_credentials_number = None
        self.doorflow_access_pin = None
        self.doorflow_system_id = None
        self.doorflow_status = None
        self.nexudus_user_id = None

        self.get_nexudus_user_info()
        if self.email_address is None:
            logging.info(
                f'{self.nexudus_coworker_id} - Could Not Create coworker')
            logging.info(
                f'{self.nexudus_coworker_id} - Failed to Retrieve Nexudus Info')
            return

        if self.nexudus_user_id is None:
            logging.info(
                f'{self.nexudus_coworker_id} - {self.first_name} {self.last_name}')
            logging.info(
                f'{self.nexudus_coworker_id} - User is a contact, not a real member')
            logging.info(f'{self.nexudus_coworker_id} - Skipping any updates')
            return

        logging.info(
            f"{self.nexudus_coworker_id} - Processing Info for Nexudus User: {self.first_name} {self.last_name}")

        self.get_nexudus_invoices()
        self.get_doorflow_user_info()
        if self.doorflow_user_id is None:
            logging.info(
                f'{self.nexudus_coworker_id} - No associated Doorflow User found')

    def get_nexudus_user_info(self):
        url = f"https://spaces.nexudus.com/api/spaces/coworkers/{self.nexudus_coworker_id}"
        coworker = requests.get(url, auth=nexudus_auth)
        if coworker.status_code == 200:
            logging.debug(
                f'{self.nexudus_coworker_id} - Successfully Requested Users info from Nexudus')
        else:
            logging.debug(
                f'{self.nexudus_coworker_id} - Failed Requested Users info from Nexudus for user')
            logging.debug(
                f'{self.nexudus_coworker_id} - {coworker.status_code}')
            logging.debug(f'{self.nexudus_coworker_id} - {coworker.reason}')
            return
        coworker = coworker.json()

        self.nexudus_key_fob_number = coworker['KeyFobNumber']
        self.nexudus_credentials_number = coworker['AccessCardId']
        self.nexudus_access_pin = coworker['AccessPincode']
        self.email_address = coworker['Email']
        self.first_name = coworker['GuessedFirstName']
        self.last_name = coworker['GuessedLastName']
        self.nexudus_teams = coworker['Teams']
        self.nexudus_contracts = coworker['CoworkerContractIds']
        self.nexudus_user_id = coworker['UserId']

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

    def get_doorflow_user_info(self):

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
                payload["credentials_number"] = int(
                    self.nexudus_credentials_number)
            except ValueError:
                if len(self.nexudus_credentials_number) == 0:
                    pass
                else:
                    logging.info('Cannot Process Credential Number')

        if self.nexudus_key_fob_number != None:
            try:
                payload["key_fob_number"] = int(
                    self.nexudus_key_fob_number)
            except ValueError:
                if len(self.nexudus_key_fob_number) == 0:
                    pass
                else:
                    logging.info('Cannot Process Key Fob Number')

        if self.nexudus_access_pin != None:
            try:
                payload['pin'] = int(self.nexudus_access_pin)
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

    def update_doorflow(self):

        if self.nexudus_user_id is None:
            logging.info(
                f"{self.nexudus_coworker_id} - Not a real User in Nexudus. Skipping Updates")
            return

        if not self.check_if_update_needed():
            logging.info(
                f'{self.nexudus_coworker_id} - Doorflow Update Not Needed for {self.first_name} {self.last_name}')
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
                    f'{self.nexudus_coworker_id} - {self.first_name} {self.last_name} created in Doorflow')
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
            else:
                logging.info(
                    f'{self.nexudus_coworker_id} - Failed to update user in Doorflow')
                logging.info(f'{self.nexudus_coworker_id} - {r.status_code}')
                logging.info(f'{self.nexudus_coworker_id} - {r.text}')


def update_all_users():

    url = f"https://spaces.nexudus.com/api/spaces/coworkers?size=10000"
    r = requests.get(url, auth=nexudus_auth)
    r = r.json()
    all_ids = [x['Id'] for x in r['Records']]

    # all_ids = [1415604543]

    for i in all_ids:
        user = coworker(i)
        user.update_doorflow()
        time.sleep(1)
        print('---------------------')


if __name__ == "__main__":

    update_all_users()

    # check_list = [1415942555,
    #               1415942541,
    #               1415942505,
    #               1415942358,
    #               1415942356,
    #               1415942350,
    #               1415940618,
    #               1415940517,
    #               1415938425,
    #               1415929851,
    #               1415926691,
    #               1415922405,
    #               1415922404,
    #               1415922306,
    #               1415922302,
    #               1415922293,
    #               1415921952,
    #               1415921700,
    #               1415921276,
    #               1415921033]

    # # check_list = [1415922302,
    # #               1415921033]

    # # check_list = [1415942505]

    # for id in check_list:
    #     user = coworker(id)
    #     user.update_doorflow()
    #     time.sleep(1)
    #     print('---------------------')
