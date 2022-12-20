# nexudus-doorflow-sync
Utility to sync Nexudus teams to DoorFlow access permissions

update the following config file options before running
nexudus-api-username
nexudus-api-password

System Overview:
An ngrok server listens for webhook messages sent by Nexudus and relays them to syncServer.py which uses syncTools.py to query the current state of a user/team in Nexudus and pushes the information to DoorFlow using a REST API. sync_readers.py runs in the background and periodically forces a sync between the DoorFlow server and the local database on the card readers.

All code is run as a service on a dedicated machine on the Lowell Makes server.
