---- MODULE Saga ----
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Step1, Step2, Step3, MaxAttempts

Plan == <<Step1, Step2, Step3>>
EffectIds == {Plan[i] : i \in 1..Len(Plan)}
States == {"unseen", "pending", "success", "failed", "compensated"}
Phases == {"forward", "compensating", "completed"}

ASSUME /\ Len(Plan) > 0
       /\ Cardinality(EffectIds) = Len(Plan)
       /\ MaxAttempts >= 2

VARIABLES status, nextStep, phase, compensationStep, attempts

vars == <<status, nextStep, phase, compensationStep, attempts>>

Init ==
    /\ status = [e \in EffectIds |-> "unseen"]
    /\ attempts = [e \in EffectIds |-> 0]
    /\ nextStep = 1
    /\ phase = "forward"
    /\ compensationStep = 0

BeginEffect ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ status[Plan[nextStep]] = "unseen"
    /\ status' = [status EXCEPT ![Plan[nextStep]] = "pending"]
    /\ attempts' = [attempts EXCEPT ![Plan[nextStep]] = @ + 1]
    /\ UNCHANGED <<nextStep, phase, compensationStep>>

RetryPending ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ status[Plan[nextStep]] = "pending"
    /\ attempts[Plan[nextStep]] < MaxAttempts
    /\ attempts' = [attempts EXCEPT ![Plan[nextStep]] = @ + 1]
    /\ UNCHANGED <<status, nextStep, phase, compensationStep>>

ForwardSuccess ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ status[Plan[nextStep]] = "pending"
    /\ status' = [status EXCEPT ![Plan[nextStep]] = "success"]
    /\ nextStep' = nextStep + 1
    /\ UNCHANGED <<phase, compensationStep, attempts>>

ForwardFailure ==
    /\ phase = "forward"
    /\ nextStep <= Len(Plan)
    /\ status[Plan[nextStep]] = "pending"
    /\ status' = [status EXCEPT ![Plan[nextStep]] = "failed"]
    /\ phase' = "compensating"
    /\ compensationStep' = nextStep - 1
    /\ UNCHANGED <<nextStep, attempts>>

CompleteForward ==
    /\ phase = "forward"
    /\ nextStep > Len(Plan)
    /\ phase' = "completed"
    /\ UNCHANGED <<status, nextStep, compensationStep, attempts>>

Compensate ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ status[Plan[compensationStep]] = "success"
    /\ status' = [status EXCEPT ![Plan[compensationStep]] = "compensated"]
    /\ compensationStep' = compensationStep - 1
    /\ UNCHANGED <<nextStep, phase, attempts>>

SkipCompensation ==
    /\ phase = "compensating"
    /\ compensationStep >= 1
    /\ status[Plan[compensationStep]] # "success"
    /\ compensationStep' = compensationStep - 1
    /\ UNCHANGED <<status, nextStep, phase, attempts>>

CompleteCompensation ==
    /\ phase = "compensating"
    /\ compensationStep = 0
    /\ phase' = "completed"
    /\ UNCHANGED <<status, nextStep, compensationStep, attempts>>

Next ==
    \/ BeginEffect
    \/ RetryPending
    \/ ForwardSuccess
    \/ ForwardFailure
    \/ CompleteForward
    \/ Compensate
    \/ SkipCompensation
    \/ CompleteCompensation

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ status \in [EffectIds -> States]
    /\ attempts \in [EffectIds -> 0..MaxAttempts]
    /\ nextStep \in 1..(Len(Plan) + 1)
    /\ phase \in Phases
    /\ compensationStep \in 0..Len(Plan)

SourceOrder ==
    \A i, j \in 1..Len(Plan) :
        (j < i /\ status[Plan[i]] # "unseen")
        => status[Plan[j]] \in {"success", "compensated"}

CompensationOrder ==
    phase = "compensating"
    => \A i, j \in 1..Len(Plan) :
        (i < j /\ status[Plan[i]] = "compensated")
        => status[Plan[j]] # "success"

====
