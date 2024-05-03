# nexudus-doorflow-sync
Utility to sync Nexudus teams to DoorFlow access permissions

Update the keys/login info in secrets.py before proceeding

The main code loop is in newSync.py and the flow is
- Every X seconds check for member accounts that have been updated on Nexudus
- If there are updates, send the updates to Doorflow backend
- After all updates have been pushed to the Doorflow backend, push a sync command to push the updates to readers
- Wait until the next cycle

This should be run as a service on a local LM machiene.

update_all_users.py is a utility that queries every user in the Nexudus database and pushes all changes to Doorflow. This can take a long time (>1 hour) but it is useful when things have gone wrong, to make sure everything is synced up.

The last thing is team_group_map.csv
This is necessary to link the teams in Nexudus to the groups in Doorflow. I wish there was a better way to do this, but this is the best I have found so far.
