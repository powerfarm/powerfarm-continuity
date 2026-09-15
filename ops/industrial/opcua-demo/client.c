#include <open62541/client.h>
#include <open62541/client_config_default.h>
#include <open62541/client_highlevel.h>
#include <stdio.h>
#include <stdlib.h>

int main(void) {
    UA_Client *client = UA_Client_new();
    if(!client) return EXIT_FAILURE;
    UA_ClientConfig_setDefault(UA_Client_getConfig(client));

    UA_StatusCode rc = UA_Client_connect(client, "opc.tcp://127.0.0.1:4840");
    if(rc != UA_STATUSCODE_GOOD) {
        fprintf(stderr, "connect failed: %s\n", UA_StatusCode_name(rc));
        UA_Client_delete(client);
        return EXIT_FAILURE;
    }

    UA_Variant value;
    UA_Variant_init(&value);
    rc = UA_Client_readValueAttribute(client, UA_NODEID_STRING(1, "powerfarm.temperature"), &value);
    if(rc != UA_STATUSCODE_GOOD || !UA_Variant_hasScalarType(&value, &UA_TYPES[UA_TYPES_DOUBLE])) {
        fprintf(stderr, "read failed: %s\n", UA_StatusCode_name(rc));
        UA_Variant_clear(&value);
        UA_Client_disconnect(client);
        UA_Client_delete(client);
        return EXIT_FAILURE;
    }

    double v = *(UA_Double *)value.data;
    printf("{\"node\":\"ns=1;s=powerfarm.temperature\",\"value\":%.2f}\n", v);

    UA_Variant_clear(&value);
    UA_Client_disconnect(client);
    UA_Client_delete(client);
    return EXIT_SUCCESS;
}
