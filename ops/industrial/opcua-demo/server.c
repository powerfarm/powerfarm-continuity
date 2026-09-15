#include <open62541/server.h>
#include <open62541/server_config_default.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>

static UA_Boolean running = true;
static void stop_handler(int sig) { (void)sig; running = false; }

int main(void) {
    signal(SIGINT, stop_handler);
    signal(SIGTERM, stop_handler);

    UA_Server *server = UA_Server_new();
    if(!server) return EXIT_FAILURE;
    UA_ServerConfig_setDefault(UA_Server_getConfig(server));

    UA_VariableAttributes attr = UA_VariableAttributes_default;
    UA_Double temperature = 23.75;
    UA_Variant_setScalar(&attr.value, &temperature, &UA_TYPES[UA_TYPES_DOUBLE]);
    attr.displayName = UA_LOCALIZEDTEXT("en-US", "Temperature");
    attr.description = UA_LOCALIZEDTEXT("en-US", "POWERFARM demo temperature");
    attr.dataType = UA_TYPES[UA_TYPES_DOUBLE].typeId;
    attr.accessLevel = UA_ACCESSLEVELMASK_READ | UA_ACCESSLEVELMASK_WRITE;

    UA_NodeId nodeId = UA_NODEID_STRING(1, "powerfarm.temperature");
    UA_QualifiedName name = UA_QUALIFIEDNAME(1, "Temperature");
    UA_StatusCode rc = UA_Server_addVariableNode(
        server,
        nodeId,
        UA_NS0ID(OBJECTSFOLDER),
        UA_NS0ID(ORGANIZES),
        name,
        UA_NS0ID(BASEDATAVARIABLETYPE),
        attr,
        NULL,
        NULL
    );
    if(rc != UA_STATUSCODE_GOOD) {
        fprintf(stderr, "add node failed: %s\n", UA_StatusCode_name(rc));
        UA_Server_delete(server);
        return EXIT_FAILURE;
    }

    fprintf(stdout, "opc.tcp://127.0.0.1:4840 node ns=1;s=powerfarm.temperature value=23.75\n");
    fflush(stdout);
    rc = UA_Server_run(server, &running);
    UA_Server_delete(server);
    return rc == UA_STATUSCODE_GOOD ? EXIT_SUCCESS : EXIT_FAILURE;
}
