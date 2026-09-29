import Foundation
import EventKit

// In-memory native objects only: these tests never request access or save events.
func expect(_ condition: @autoclosure () -> Bool, _ message: String) {
    if !condition() { fatalError(message) }
}
func expectRejection(_ code: String, _ body: () throws -> Void) {
    do { try body(); fatalError("Expected rejection: \(code)") }
    catch let error as ProofError { expect(error.code == code, "Wrong rejection: \(error.code)") }
    catch { fatalError("Unexpected error: \(error)") }
}
let ref = Reference(calendar: "calendar/α", item: "item+\"\\", start: 1_800_000_000.25)
let canonical = try encodeReference(ref)
let simple = Reference(calendar: "cal", item: "item", start: 1)
let expectedID = "ekp1." + Data(#"{"calendar":"cal","item":"item","start":1}"#.utf8).base64EncodedString()
expect(try! encodeReference(simple) == expectedID, "Reference bytes are not canonical")
for _ in 0..<100 {
    expect(try! encodeReference(ref) == canonical, "Repeated encoding changed the ID")
}
let equivalent = Reference(calendar: ref.calendar, item: ref.item, start: ref.start)
expect(try! encodeReference(equivalent) == canonical, "Equal references have unequal IDs")
let decoded = try decodeReference(canonical)
expect(decoded.calendar == ref.calendar && decoded.item == ref.item && decoded.start == ref.start, "Reference round-trip changed data")
// An old valid reference with unsorted object keys must still decode.
let oldJSON = #"{"start":1800000000.25,"item":"old-item","calendar":"old-calendar"}"#
let old = try decodeReference("ekp1." + Data(oldJSON.utf8).base64EncodedString())
expect(old.calendar == "old-calendar" && old.item == "old-item" && old.start == ref.start, "Legacy reference rejected")
expectRejection("INVALID_ID") { _ = try decodeReference("ekp1.invalid") }

let store = EKEventStore()
let request = Request(protocol: wireVersion, request_id: String(repeating: "a", count: 32), operation: "update", token: String(repeating: "b", count: 32), args: [:])
let recurring = EKEvent(eventStore: store)
recurring.calendar = EKCalendar(for: .event, eventStore: store)
recurring.recurrenceRules = [EKRecurrenceRule(recurrenceWith: .daily, interval: 1, end: EKRecurrenceEnd(occurrenceCount: 3))]
expect(recurring.hasRecurrenceRules, "Fixture lacks native recurrence")
expectRejection("UNSUPPORTED_OPERATION") { try owned(recurring, request) }
// Detached and provider read-only state cannot be manufactured by saving in a
// permission-free test. Exercise the shared pre-mutation policy for each state.
for writable in [false, true] {
    expectRejection("UNSUPPORTED_OPERATION") {
        try checkWriteScope(recurring: false, detached: true, writable: writable)
    }
}
expectRejection("READ_ONLY") {
    try checkWriteScope(recurring: false, detached: false, writable: false)
}
try checkWriteScope(recurring: false, detached: false, writable: true)
print("PASS: canonical IDs, backward decoding, native recurrence and detached/read-only policy (no saves)")

let alarmEvent = EKEvent(eventStore: store)
alarmEvent.alarms = [EKAlarm(relativeOffset: -Double.zero)]
let requestedAtStart = alarmState(alarmEvent)
alarmEvent.alarms = [EKAlarm(relativeOffset: Double.zero)]
expect(alarmState(alarmEvent) == requestedAtStart, "Signed zero must not fail at-start readback")
alarmEvent.alarms = [EKAlarm(absoluteDate: Date(timeIntervalSince1970: 1_800_000_000))]
expect(alarmState(alarmEvent) != requestedAtStart, "Absolute and relative alarms must remain distinct")
alarmEvent.alarms = [EKAlarm(relativeOffset: -60)]
expect(alarmState(alarmEvent) != requestedAtStart, "Different offsets must fail comparison")
print("PASS: at-start signed-zero equivalence without losing absolute/relative semantics")
