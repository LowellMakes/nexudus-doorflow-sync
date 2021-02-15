import requests
import pandas as pd
import time
import configargparse
import os
import datetime

class AllUpdates():
    """docstring for AllUpdates"""
    def __init__(self,config):
        self.updates = []
        self.config = config

    def add_update(self, update):
        url = 'https://api.doorflow.com/api/2/person/{}'.format(update[0])
        shops_id = update[1]

        payload = {'group_ids':shops_id}
        self.updates.append((url,payload))

    def push_updates(self):

        doorflow_auth = requests.auth.HTTPBasicAuth(self.config.doorflow_api_key,'x')
        if len(self.updates) == 0:
            return

        update = self.updates[0]
        r = requests.put(update[0], auth=doorflow_auth, json=update[1])
        if r.status_code == 201:
            url = 'https://api.doorflow.com/api/2/sync'
            requests.post(url, auth=doorflow_auth)
            self.updates = []
            print(datetime.datetime.now(),'Success:', update)

class Nexudus():
    """docstring for Nexudus"""
    def __init__(self, config):

        self.config = config
        self.users = None

        self.update()

    def update(self):
        nexudus_auth = (self.config.nexudus_api_username,self.config.nexudus_api_password)
        url = "https://spaces.nexudus.com/api/spaces/coworkers?size=500"
        all_users = requests.get(url, auth=nexudus_auth)
        if all_users.status_code != 200:
            raise StatusError
        self.users = pd.DataFrame(all_users.json()['Records'])

    def check_doorflow_status(self, user, doorflow):
        if user.TeamNames == None:
            # print(user.FullName, 'is not on any Teams')
            return False

        # check if their doorflow fob is active
        if not doorflow.users[doorflow.users.email==user.Email].enabled.iloc[0]:
            return False

        nexudus_teams = str(user.TeamNames)
        nexudus_teams = nexudus_teams.split(',')

        new_doorflow_groups = []
        for team in nexudus_teams:
            team = team.strip()
            if team in doorflow.group_map.keys():
                new_doorflow_groups.append(doorflow.group_map[team])

        # print(user.FullName, 'is in the following Nexudus Teams:', new_doorflow_groups)

        current_doorflow_groups = []
        for group in doorflow.users[doorflow.users.email==user.Email].groups.iloc[0]:
            if group['id'] == 4482:
                continue
            current_doorflow_groups.append(group['id'])

        # print(user.FullName, 'is in the following DoorFlow Groups:', current_doorflow_groups)

        if set(current_doorflow_groups) == set(new_doorflow_groups):
            return False

        user_doorflow_id = doorflow.users[doorflow.users.email==user.Email].id.iloc[0]
        user_doorflow_name = doorflow.users[doorflow.users.email==user.Email].FullName.iloc[0]
        new_doorflow_groups.append(4482)
        # if user_doorflow_id == 401856:
        #     breakpoint()
        #     return False

        print('Pending:',user_doorflow_name, new_doorflow_groups)

        return user_doorflow_id, new_doorflow_groups

class DoorFlow():
    """docstring for DoorFlow"""
    def __init__(self, config):

        self.config = config
        self.users = None
        self.group_map = {}
        self.doorflow_auth = requests.auth.HTTPBasicAuth(config.doorflow_api_key,'x')

        self.update_group_map()
        self.update()

    def update(self):
        url = "https://admin.doorflow.com/api/2/people"
        doorflow_users = requests.get(url, auth = self.doorflow_auth)
        if doorflow_users.status_code != 200:
            raise StatusError
        doorflow_users = pd.DataFrame(doorflow_users.json())
        doorflow_users['FullName'] = doorflow_users.first_name + ' ' + doorflow_users.last_name
        self.users = doorflow_users

    def update_group_map(self):
        url = "https://admin.doorflow.com/api/2/groups"
        doorflow_groups = requests.get(url, auth=self.doorflow_auth)
        doorflow_groups = doorflow_groups.json()

        for g in doorflow_groups:
            if g['name'] == 'Basic Member':
                self.group_map['Basic Member']=g['id']
            if g['name'] in self.config.group_list:
                self.group_map[g['name']]=g['id']

def sync(config):

    nexudus = Nexudus(config)
    doorflow = DoorFlow(config)
    updates = AllUpdates(config)

    all_nexudus_users = nexudus.users

    while True:

        for index,user in all_nexudus_users.iterrows():
            update_status = nexudus.check_doorflow_status(user,doorflow)
            if update_status != False:
                updates.add_update(update_status)

        updates.push_updates()

        try:
            nexudus.update()
        except:
            print('Something wrong with Nexudus Update')

        try:
            doorflow.update()
        except:
            print('Something wrong with DoorFlow update')

        print(datetime.datetime.now(),'Done with Updates')
        time.sleep(config.cycle_time)

def main():

    default_config_file = os.path.join(os.path.dirname(os.path.realpath(__file__)),'nexudus_doorflow_sync.config')

    p = configargparse.ArgParser()
    p.add('--config-file', is_config_file=True, help='config file path', default=default_config_file)
    p.add('-t','--cycle-time', type=int, help='time, in seconds, between sync')
    p.add('--team-list', action='append', help='list of Nexudus Teams to sync')
    p.add('--group-list', action='append', help='list of DoorFlow groups to sync')
    p.add('--doorflow-api-key', help='API key for DoorFlow')
    p.add('--nexudus-api-username', help='Username for Nexudus')
    p.add('--nexudus-api-password', help='Password for Nexudus')

    config = p.parse_args()

    if len(config.team_list) != len(config.group_list):
        print('Team list and Group list and not the same length')
        print('Check the config file')
        print('Aborting')
        return

    sync(config)

if __name__ == '__main__':
    main()
