
import json
import multiprocessing
import time
from multiprocessing import Process
import processes

import pandas as pd
import requests
from passwords import secrets
from flask import Flask, Response, request
import syncTools

team_map = pd.read_csv('team_group_map.csv', dtype='str')
app = Flask(__name__)

doorflow_auth = requests.auth.HTTPBasicAuth(secrets['doorflow_auth_key'], 'x')
nexudus_auth = (secrets['nexudus_username'],
                secrets['nexudus_password'])

pool = multiprocessing.Pool(5)


@app.route('/AccessControlUpdate', methods=['POST'])
def update_access_control():

    coworker = request.json[0]
    coworker_id = coworker['Id']

    pool.map(processes.updateProcess, (coworker_id,))

    return Response(status=200)


@app.route('/InvoicePaid', methods=['POST'])
def invoice_paid():

    coworker = request.json[0]
    coworker_id = coworker['Id']

    pool.map(processes.updateProcess, (coworker_id,))
    return Response(status=200)


@app.route('/InvoiceIssued', methods=['POST'])
def invoice_issued():

    coworker = request.json[0]
    coworker_id = coworker['Id']

    pool.map(processes.updateProcess, (coworker_id,))
    return Response(status=200)


@app.route('/MembershipExpired', methods=['POST'])
def expired_membership():

    coworker = request.json[0]
    coworker_id = coworker['Id']

    pool.map(processes.updateProcess, (coworker_id,))
    return Response(status=200)


@app.route('/TeamUpdate', methods=['POST'])
def team_update():

    team = request.json[0]
    pool.map(processes.update_team_members, (team,))
    return Response(status=200)


@app.route('/Checkin', methods=['POST'])
def checkin_user():

    return Response(status=200)


if __name__ == '__main__':
    app.run(debug=True, port=5500)
