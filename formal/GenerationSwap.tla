---- MODULE GenerationSwap ----
EXTENDS FiniteSets

CONSTANTS Generations, Requests, InitialGeneration

NoGeneration == "none"
CandidateStates == {"absent", "unknown", "valid", "invalid", "conflict", "published"}
ProcessPhases == {"running", "draining", "stopped"}

ASSUME /\ InitialGeneration \in Generations
       /\ NoGeneration \notin Generations

VARIABLES active, candidateBase, candidateState, validated, executionGeneration, phase

vars == <<active, candidateBase, candidateState, validated, executionGeneration, phase>>

Init ==
    /\ active = InitialGeneration
    /\ candidateBase = [g \in Generations |-> NoGeneration]
    /\ candidateState = [g \in Generations |-> "absent"]
    /\ validated = {InitialGeneration}
    /\ executionGeneration = [r \in Requests |-> NoGeneration]
    /\ phase = "running"

BuildCandidate(g) ==
    /\ phase = "running"
    /\ g \in Generations
    /\ g # active
    /\ candidateState[g] \in {"absent", "invalid", "conflict", "published"}
    /\ candidateBase' = [candidateBase EXCEPT ![g] = active]
    /\ candidateState' = [candidateState EXCEPT ![g] = "unknown"]
    /\ UNCHANGED <<active, validated, executionGeneration, phase>>

ValidateCandidate(g) ==
    /\ phase = "running"
    /\ g \in Generations
    /\ candidateState[g] = "unknown"
    /\ candidateState' = [candidateState EXCEPT ![g] = "valid"]
    /\ validated' = validated \cup {g}
    /\ UNCHANGED <<active, candidateBase, executionGeneration, phase>>

RejectCandidate(g) ==
    /\ phase = "running"
    /\ g \in Generations
    /\ candidateState[g] = "unknown"
    /\ candidateState' = [candidateState EXCEPT ![g] = "invalid"]
    /\ UNCHANGED <<active, candidateBase, validated, executionGeneration, phase>>

PublishCandidate(g) ==
    /\ phase = "running"
    /\ g \in Generations
    /\ candidateState[g] = "valid"
    /\ g \in validated
    /\ active = candidateBase[g]
    /\ active' = g
    /\ candidateState' = [candidateState EXCEPT ![g] = "published"]
    /\ UNCHANGED <<candidateBase, validated, executionGeneration, phase>>

RejectStaleCandidate(g) ==
    /\ phase = "running"
    /\ g \in Generations
    /\ candidateState[g] = "valid"
    /\ active # candidateBase[g]
    /\ candidateState' = [candidateState EXCEPT ![g] = "conflict"]
    /\ UNCHANGED <<active, candidateBase, validated, executionGeneration, phase>>

StartExecution(r) ==
    /\ phase = "running"
    /\ r \in Requests
    /\ executionGeneration[r] = NoGeneration
    /\ executionGeneration' = [executionGeneration EXCEPT ![r] = active]
    /\ UNCHANGED <<active, candidateBase, candidateState, validated, phase>>

BeginDrain ==
    /\ phase = "running"
    /\ phase' = "draining"
    /\ UNCHANGED <<active, candidateBase, candidateState, validated, executionGeneration>>

Stop ==
    /\ phase = "draining"
    /\ phase' = "stopped"
    /\ UNCHANGED <<active, candidateBase, candidateState, validated, executionGeneration>>

Next ==
    \/ \E g \in Generations : BuildCandidate(g)
    \/ \E g \in Generations : ValidateCandidate(g)
    \/ \E g \in Generations : RejectCandidate(g)
    \/ \E g \in Generations : PublishCandidate(g)
    \/ \E g \in Generations : RejectStaleCandidate(g)
    \/ \E r \in Requests : StartExecution(r)
    \/ BeginDrain
    \/ Stop

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ active \in Generations
    /\ candidateBase \in [Generations -> Generations \cup {NoGeneration}]
    /\ candidateState \in [Generations -> CandidateStates]
    /\ validated \subseteq Generations
    /\ executionGeneration \in [Requests -> Generations \cup {NoGeneration}]
    /\ phase \in ProcessPhases

PublishedGenerationWasValidated == active \in validated

ExecutionUsesOneValidatedGeneration ==
    \A r \in Requests :
        executionGeneration[r] # NoGeneration
        => executionGeneration[r] \in validated

PublishedCandidateMatchedItsBase ==
    \A g \in Generations :
        candidateState[g] = "published"
        => candidateBase[g] \in Generations

====
