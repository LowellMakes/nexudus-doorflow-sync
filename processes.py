
import syncTools

def updateProcess(coworker_id):

    user = syncTools.coworker(coworker_id)
    user.update_doorflow()


def update_team_members(team_json):

    for coworker_id in team_json['TeamMembers']:
        updateProcess(coworker_id)
