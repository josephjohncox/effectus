---- MODULE GenerationSwap ----
EXTENDS FiniteSets

CONSTANTS Generations, Requests, InitialGeneration

NoGeneration == "none"
ValidationStates == {"unknown", "valid", "invalid"}

ASSUME /\ InitialGeneration \in Generations
       /\ NoGeneration \notin Generations

VARIABLES active, candidate, validation, validated, executionGeneration

vars == <<active, candidate, validation, validated, executionGeneration>>

Init ==
    /\ active = InitialGeneration
    /\ candidate = NoGeneration
    /\ validation = "unknown"
    /\ validated = {InitialGeneration}
    /\ executionGeneration = [r \in Requests |-> NoGeneration]

BuildCandidate(g) ==
    /\ g \in Generations
    /\ g # active
    /\ candidate' = g
    /\ validation' = "unknown"
    /\ UNCHANGED <<active, validated, executionGeneration>>

ValidateCandidate ==
    /\ candidate \in Generations
    /\ validation = "unknown"
    /\ validation' = "valid"
    /\ validated' = validated \cup {candidate}
    /\ UNCHANGED <<active, candidate, executionGeneration>>

RejectCandidate ==
    /\ candidate \in Generations
    /\ validation = "unknown"
    /\ candidate' = NoGeneration
    /\ validation' = "invalid"
    /\ UNCHANGED <<active, validated, executionGeneration>>

PublishCandidate ==
    /\ candidate \in Generations
    /\ validation = "valid"
    /\ candidate \in validated
    /\ active' = candidate
    /\ candidate' = NoGeneration
    /\ validation' = "unknown"
    /\ UNCHANGED <<validated, executionGeneration>>

StartExecution(r) ==
    /\ r \in Requests
    /\ executionGeneration[r] = NoGeneration
    /\ executionGeneration' = [executionGeneration EXCEPT ![r] = active]
    /\ UNCHANGED <<active, candidate, validation, validated>>

Next ==
    \/ \E g \in Generations : BuildCandidate(g)
    \/ ValidateCandidate
    \/ RejectCandidate
    \/ PublishCandidate
    \/ \E r \in Requests : StartExecution(r)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ active \in Generations
    /\ candidate \in Generations \cup {NoGeneration}
    /\ validation \in ValidationStates
    /\ validated \subseteq Generations
    /\ executionGeneration \in [Requests -> Generations \cup {NoGeneration}]

PublishedGenerationWasValidated == active \in validated

ExecutionUsesPublishedGeneration ==
    \A r \in Requests :
        executionGeneration[r] # NoGeneration
        => executionGeneration[r] \in validated

====
