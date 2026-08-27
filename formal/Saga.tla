---- MODULE Saga ----
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Step1, Step2, Step3, MaxAttempts, MaxToken, MaxStale, Workers, NoWorker

Plan == <<Step1, Step2, Step3>>
EffectIds == {Plan[i] : i \in 1..Len(Plan)}
EffectStates == {"unseen", "pending", "success", "failed", "compensated"}
DispatchStates == {"absent", "ready", "leased", "succeeded", "failed"}
OutcomeClasses == {"none", "success", "retryable", "permanent", "unknown", "fence", "dependency"}
BlockedPhases == {"blocked_unknown", "blocked_fence", "blocked_dependency"}
TerminalPhases == {"completed_success", "completed_compensated", "compensation_failed"}
Phases == {"forward", "compensating"} \cup BlockedPhases \cup TerminalPhases
Directions == {"none", "forward", "reverse"}

ASSUME /\ Len(Plan) > 0
       /\ Cardinality(EffectIds) = Len(Plan)
       /\ MaxAttempts >= 2
       /\ MaxToken >= MaxAttempts
       /\ MaxStale >= 1
       /\ NoWorker \notin Workers

VARIABLES status, phase, nextStep, compensationStep,
          forwardDispatch, reverseDispatch,
          forwardAttempts, reverseAttempts,
          forwardToken, reverseToken,
          forwardOwner, reverseOwner,
          forwardOutcome, reverseOutcome,
          forwardAcceptedToken, reverseAcceptedToken,
          blockedDirection, blockedStep, staleCompletions

vars == <<status, phase, nextStep, compensationStep,
          forwardDispatch, reverseDispatch,
          forwardAttempts, reverseAttempts,
          forwardToken, reverseToken,
          forwardOwner, reverseOwner,
          forwardOutcome, reverseOutcome,
          forwardAcceptedToken, reverseAcceptedToken,
          blockedDirection, blockedStep, staleCompletions>>

Init ==
    /\ status = [e \in EffectIds |-> "unseen"]
    /\ phase = "forward"
    /\ nextStep = 1
    /\ compensationStep = 0
    /\ forwardDispatch = [e \in EffectIds |-> "absent"]
    /\ reverseDispatch = [e \in EffectIds |-> "absent"]
    /\ forwardAttempts = [e \in EffectIds |-> 0]
    /\ reverseAttempts = [e \in EffectIds |-> 0]
    /\ forwardToken = [e \in EffectIds |-> 0]
    /\ reverseToken = [e \in EffectIds |-> 0]
    /\ forwardOwner = [e \in EffectIds |-> NoWorker]
    /\ reverseOwner = [e \in EffectIds |-> NoWorker]
    /\ forwardOutcome = [e \in EffectIds |-> "none"]
    /\ reverseOutcome = [e \in EffectIds |-> "none"]
    /\ forwardAcceptedToken = [e \in EffectIds |-> 0]
    /\ reverseAcceptedToken = [e \in EffectIds |-> 0]
    /\ blockedDirection = "none"
    /\ blockedStep = 0
    /\ staleCompletions = 0

PrepareForward ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ status[e] = "unseen"
       /\ forwardDispatch[e] = "absent"
       /\ status' = [status EXCEPT ![e] = "pending"]
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "ready"]
    /\ UNCHANGED <<phase, nextStep, compensationStep, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

LeaseForward(w) ==
    /\ phase = "forward"
    /\ w \in Workers
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "ready"
       /\ forwardAttempts[e] < MaxAttempts
       /\ forwardToken[e] < MaxToken
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "leased"]
       /\ forwardAttempts' = [forwardAttempts EXCEPT ![e] = @ + 1]
       /\ forwardToken' = [forwardToken EXCEPT ![e] = @ + 1]
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = w]
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "none"]
    /\ UNCHANGED <<status, phase, nextStep, compensationStep, reverseDispatch,
                    reverseAttempts, reverseToken, reverseOwner, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

ExpireForwardLease ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "leased"
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = NoWorker]
       /\ IF forwardAttempts[e] < MaxAttempts
             THEN /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "ready"]
                  /\ UNCHANGED <<phase, forwardOutcome, forwardAcceptedToken,
                                  blockedDirection, blockedStep>>
             ELSE /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "failed"]
                  /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "unknown"]
                  /\ forwardAcceptedToken' = [forwardAcceptedToken EXCEPT ![e] = forwardToken[e]]
                  /\ phase' = "blocked_unknown"
                  /\ blockedDirection' = "forward"
                  /\ blockedStep' = nextStep
    /\ UNCHANGED <<status, nextStep, compensationStep, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    reverseOwner, reverseOutcome, reverseAcceptedToken, staleCompletions>>

ForwardSuccess(w, token) ==
    /\ phase = "forward"
    /\ w \in Workers
    /\ token \in 1..MaxToken
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "leased"
       /\ forwardOwner[e] = w
       /\ forwardToken[e] = token
       /\ status' = [status EXCEPT ![e] = "success"]
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "succeeded"]
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = NoWorker]
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "success"]
       /\ forwardAcceptedToken' = [forwardAcceptedToken EXCEPT ![e] = forwardToken[e]]
       /\ nextStep' = nextStep + 1
    /\ UNCHANGED <<phase, compensationStep, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    reverseOwner, reverseOutcome, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

ForwardRetryable ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "leased"
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "retryable"]
       /\ forwardAcceptedToken' = [forwardAcceptedToken EXCEPT ![e] = forwardToken[e]]
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = NoWorker]
       /\ IF forwardAttempts[e] < MaxAttempts
             THEN /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "ready"]
                  /\ UNCHANGED <<status, phase, compensationStep>>
             ELSE /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "failed"]
                  /\ status' = [status EXCEPT ![e] = "failed"]
                  /\ phase' = "compensating"
                  /\ compensationStep' = nextStep - 1
    /\ UNCHANGED <<nextStep, reverseDispatch, forwardAttempts, reverseAttempts,
                    forwardToken, reverseToken, reverseOwner, reverseOutcome,
                    reverseAcceptedToken, blockedDirection, blockedStep, staleCompletions>>

ForwardPermanentFailure ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "leased"
       /\ status' = [status EXCEPT ![e] = "failed"]
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "failed"]
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = NoWorker]
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "permanent"]
       /\ forwardAcceptedToken' = [forwardAcceptedToken EXCEPT ![e] = forwardToken[e]]
       /\ phase' = "compensating"
       /\ compensationStep' = nextStep - 1
    /\ UNCHANGED <<nextStep, reverseDispatch, forwardAttempts, reverseAttempts,
                    forwardToken, reverseToken, reverseOwner, reverseOutcome,
                    reverseAcceptedToken, blockedDirection, blockedStep, staleCompletions>>

BlockedPhase(outcome) ==
    CASE outcome = "unknown" -> "blocked_unknown"
      [] outcome = "fence" -> "blocked_fence"
      [] outcome = "dependency" -> "blocked_dependency"

BlockForward(outcome) ==
    /\ outcome \in {"unknown", "fence", "dependency"}
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ LET e == Plan[nextStep] IN
       /\ forwardDispatch[e] = "leased"
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "failed"]
       /\ forwardOwner' = [forwardOwner EXCEPT ![e] = NoWorker]
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = outcome]
       /\ forwardAcceptedToken' = [forwardAcceptedToken EXCEPT ![e] = forwardToken[e]]
       /\ phase' = BlockedPhase(outcome)
       /\ blockedDirection' = "forward"
       /\ blockedStep' = nextStep
    /\ UNCHANGED <<status, nextStep, compensationStep, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    reverseOwner, reverseOutcome, reverseAcceptedToken, staleCompletions>>

RecoverBlockedForward ==
    /\ phase \in BlockedPhases
    /\ blockedDirection = "forward"
    /\ blockedStep = nextStep
    /\ LET e == Plan[blockedStep] IN
       /\ forwardAttempts[e] < MaxAttempts
       /\ forwardDispatch[e] = "failed"
       /\ forwardDispatch' = [forwardDispatch EXCEPT ![e] = "ready"]
       /\ forwardOutcome' = [forwardOutcome EXCEPT ![e] = "none"]
       /\ phase' = "forward"
       /\ blockedDirection' = "none"
       /\ blockedStep' = 0
    /\ UNCHANGED <<status, nextStep, compensationStep, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken, staleCompletions>>

CompleteForward ==
    /\ phase = "forward"
    /\ nextStep > Len(Plan)
    /\ phase' = "completed_success"
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

PrepareReverse ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ status[e] = "success"
       /\ reverseDispatch[e] = "absent"
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "ready"]
    /\ UNCHANGED <<status, phase, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

LeaseReverse(w) ==
    /\ phase = "compensating"
    /\ w \in Workers
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "ready"
       /\ reverseAttempts[e] < MaxAttempts
       /\ reverseToken[e] < MaxToken
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "leased"]
       /\ reverseAttempts' = [reverseAttempts EXCEPT ![e] = @ + 1]
       /\ reverseToken' = [reverseToken EXCEPT ![e] = @ + 1]
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = w]
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "none"]
    /\ UNCHANGED <<status, phase, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, forwardToken, forwardOwner, forwardOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

ExpireReverseLease ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "leased"
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = NoWorker]
       /\ IF reverseAttempts[e] < MaxAttempts
             THEN /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "ready"]
                  /\ UNCHANGED <<phase, reverseOutcome, reverseAcceptedToken,
                                  blockedDirection, blockedStep>>
             ELSE /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "failed"]
                  /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "unknown"]
                  /\ reverseAcceptedToken' = [reverseAcceptedToken EXCEPT ![e] = reverseToken[e]]
                  /\ phase' = "blocked_unknown"
                  /\ blockedDirection' = "reverse"
                  /\ blockedStep' = compensationStep
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, forwardOutcome, forwardAcceptedToken, staleCompletions>>

ReverseSuccess(w, token) ==
    /\ phase = "compensating"
    /\ w \in Workers
    /\ token \in 1..MaxToken
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "leased"
       /\ reverseOwner[e] = w
       /\ reverseToken[e] = token
       /\ status' = [status EXCEPT ![e] = "compensated"]
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "succeeded"]
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = NoWorker]
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "success"]
       /\ reverseAcceptedToken' = [reverseAcceptedToken EXCEPT ![e] = reverseToken[e]]
       /\ compensationStep' = compensationStep - 1
    /\ UNCHANGED <<phase, nextStep, forwardDispatch, forwardAttempts, reverseAttempts,
                    forwardToken, reverseToken, forwardOwner, forwardOutcome,
                    forwardAcceptedToken, blockedDirection, blockedStep, staleCompletions>>

ReverseRetryable ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "leased"
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "retryable"]
       /\ reverseAcceptedToken' = [reverseAcceptedToken EXCEPT ![e] = reverseToken[e]]
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = NoWorker]
       /\ IF reverseAttempts[e] < MaxAttempts
             THEN /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "ready"]
                  /\ UNCHANGED phase
             ELSE /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "failed"]
                  /\ phase' = "compensation_failed"
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, forwardOutcome, forwardAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

ReversePermanentFailure ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "leased"
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "failed"]
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = NoWorker]
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "permanent"]
       /\ reverseAcceptedToken' = [reverseAcceptedToken EXCEPT ![e] = reverseToken[e]]
       /\ phase' = "compensation_failed"
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, forwardOutcome, forwardAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

BlockReverse(outcome) ==
    /\ outcome \in {"unknown", "fence", "dependency"}
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ LET e == Plan[compensationStep] IN
       /\ reverseDispatch[e] = "leased"
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "failed"]
       /\ reverseOwner' = [reverseOwner EXCEPT ![e] = NoWorker]
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = outcome]
       /\ reverseAcceptedToken' = [reverseAcceptedToken EXCEPT ![e] = reverseToken[e]]
       /\ phase' = BlockedPhase(outcome)
       /\ blockedDirection' = "reverse"
       /\ blockedStep' = compensationStep
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, forwardOutcome, forwardAcceptedToken, staleCompletions>>

RecoverBlockedReverse ==
    /\ phase \in BlockedPhases
    /\ blockedDirection = "reverse"
    /\ blockedStep = compensationStep
    /\ LET e == Plan[blockedStep] IN
       /\ reverseAttempts[e] < MaxAttempts
       /\ reverseDispatch[e] = "failed"
       /\ reverseDispatch' = [reverseDispatch EXCEPT ![e] = "ready"]
       /\ reverseOutcome' = [reverseOutcome EXCEPT ![e] = "none"]
       /\ phase' = "compensating"
       /\ blockedDirection' = "none"
       /\ blockedStep' = 0
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome,
                    forwardAcceptedToken, reverseAcceptedToken, staleCompletions>>

SkipReverse ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ status[Plan[compensationStep]] # "success"
    /\ compensationStep' = compensationStep - 1
    /\ UNCHANGED <<status, phase, nextStep, forwardDispatch, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

CompleteCompensation ==
    /\ phase = "compensating"
    /\ compensationStep = 0
    /\ phase' = "completed_compensated"
    /\ UNCHANGED <<status, nextStep, compensationStep, forwardDispatch, reverseDispatch,
                    forwardAttempts, reverseAttempts, forwardToken, reverseToken,
                    forwardOwner, reverseOwner, forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep, staleCompletions>>

StaleForwardCompletion(e, w, token) ==
    /\ e \in EffectIds
    /\ w \in Workers
    /\ token \in 1..MaxToken
    /\ staleCompletions < MaxStale
    /\ \/ forwardDispatch[e] # "leased"
       \/ token # forwardToken[e]
       \/ w # forwardOwner[e]
    /\ staleCompletions' = staleCompletions + 1
    /\ UNCHANGED <<status, phase, nextStep, compensationStep,
                    forwardDispatch, reverseDispatch, forwardAttempts, reverseAttempts,
                    forwardToken, reverseToken, forwardOwner, reverseOwner,
                    forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep>>

StaleReverseCompletion(e, w, token) ==
    /\ e \in EffectIds
    /\ w \in Workers
    /\ token \in 1..MaxToken
    /\ staleCompletions < MaxStale
    /\ \/ reverseDispatch[e] # "leased"
       \/ token # reverseToken[e]
       \/ w # reverseOwner[e]
    /\ staleCompletions' = staleCompletions + 1
    /\ UNCHANGED <<status, phase, nextStep, compensationStep,
                    forwardDispatch, reverseDispatch, forwardAttempts, reverseAttempts,
                    forwardToken, reverseToken, forwardOwner, reverseOwner,
                    forwardOutcome, reverseOutcome,
                    forwardAcceptedToken, reverseAcceptedToken,
                    blockedDirection, blockedStep>>

Next ==
    \/ PrepareForward
    \/ \E w \in Workers : LeaseForward(w)
    \/ ExpireForwardLease
    \/ \E w \in Workers, token \in 1..MaxToken : ForwardSuccess(w, token)
    \/ ForwardRetryable
    \/ ForwardPermanentFailure
    \/ \E outcome \in {"unknown", "fence", "dependency"} : BlockForward(outcome)
    \/ RecoverBlockedForward
    \/ CompleteForward
    \/ PrepareReverse
    \/ \E w \in Workers : LeaseReverse(w)
    \/ ExpireReverseLease
    \/ \E w \in Workers, token \in 1..MaxToken : ReverseSuccess(w, token)
    \/ ReverseRetryable
    \/ ReversePermanentFailure
    \/ \E outcome \in {"unknown", "fence", "dependency"} : BlockReverse(outcome)
    \/ RecoverBlockedReverse
    \/ SkipReverse
    \/ CompleteCompensation
    \/ \E e \in EffectIds, w \in Workers, token \in 1..MaxToken : StaleForwardCompletion(e, w, token)
    \/ \E e \in EffectIds, w \in Workers, token \in 1..MaxToken : StaleReverseCompletion(e, w, token)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ status \in [EffectIds -> EffectStates]
    /\ phase \in Phases
    /\ nextStep \in 1..(Len(Plan) + 1)
    /\ compensationStep \in 0..Len(Plan)
    /\ forwardDispatch \in [EffectIds -> DispatchStates]
    /\ reverseDispatch \in [EffectIds -> DispatchStates]
    /\ forwardAttempts \in [EffectIds -> 0..MaxAttempts]
    /\ reverseAttempts \in [EffectIds -> 0..MaxAttempts]
    /\ forwardToken \in [EffectIds -> 0..MaxToken]
    /\ reverseToken \in [EffectIds -> 0..MaxToken]
    /\ forwardOwner \in [EffectIds -> Workers \cup {NoWorker}]
    /\ reverseOwner \in [EffectIds -> Workers \cup {NoWorker}]
    /\ forwardOutcome \in [EffectIds -> OutcomeClasses]
    /\ reverseOutcome \in [EffectIds -> OutcomeClasses]
    /\ forwardAcceptedToken \in [EffectIds -> 0..MaxToken]
    /\ reverseAcceptedToken \in [EffectIds -> 0..MaxToken]
    /\ blockedDirection \in Directions
    /\ blockedStep \in 0..Len(Plan)
    /\ staleCompletions \in 0..MaxStale

SourceOrder ==
    \A i, j \in 1..Len(Plan) :
        (j < i /\ status[Plan[i]] # "unseen")
        => status[Plan[j]] \in {"success", "compensated"}

DurableLeaseShape ==
    /\ \A e \in EffectIds :
          (forwardDispatch[e] = "leased") <=> (forwardOwner[e] \in Workers /\ forwardToken[e] > 0)
    /\ \A e \in EffectIds :
          (reverseDispatch[e] = "leased") <=> (reverseOwner[e] \in Workers /\ reverseToken[e] > 0)

AcceptedCompletionUsesCurrentToken ==
    /\ \A e \in EffectIds : forwardAcceptedToken[e] <= forwardToken[e]
    /\ \A e \in EffectIds : reverseAcceptedToken[e] <= reverseToken[e]
    /\ \A e \in EffectIds :
          forwardDispatch[e] \in {"succeeded", "failed"} => forwardAcceptedToken[e] = forwardToken[e]
    /\ \A e \in EffectIds :
          reverseDispatch[e] \in {"succeeded", "failed"} => reverseAcceptedToken[e] = reverseToken[e]

CompensationOrder ==
    \A i, j \in 1..Len(Plan) :
        (i < j /\ status[Plan[i]] = "compensated")
        => status[Plan[j]] # "success"

BlockedHasDurableOutcome ==
    phase \in BlockedPhases =>
        /\ blockedStep \in 1..Len(Plan)
        /\ blockedDirection \in {"forward", "reverse"}
        /\ LET e == Plan[blockedStep] IN
             IF blockedDirection = "forward"
             THEN /\ forwardDispatch[e] = "failed"
                  /\ forwardOutcome[e] \in {"unknown", "fence", "dependency"}
             ELSE /\ reverseDispatch[e] = "failed"
                  /\ reverseOutcome[e] \in {"unknown", "fence", "dependency"}

TerminalHasNoLease ==
    phase \in TerminalPhases =>
        /\ \A e \in EffectIds : forwardDispatch[e] # "leased"
        /\ \A e \in EffectIds : reverseDispatch[e] # "leased"

TerminalMeaning ==
    /\ phase = "completed_success" => \A e \in EffectIds : status[e] = "success"
    /\ phase = "completed_compensated" => \A e \in EffectIds : status[e] # "success"
    /\ phase = "compensation_failed" =>
          /\ compensationStep \in 1..Len(Plan)
          /\ reverseDispatch[Plan[compensationStep]] = "failed"

====
