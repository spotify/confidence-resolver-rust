package com.spotify.confidence;

import com.spotify.confidence.iam.v1.Client;
import com.spotify.confidence.iam.v1.ClientCredential;

record AccountClient(String accountName, Client client, ClientCredential clientCredential) {}
