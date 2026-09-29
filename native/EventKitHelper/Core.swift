import Foundation
import EventKit

let wireVersion = "acal-native-proof-v1"
struct Request: Decodable {
    let `protocol`: String
    let request_id: String
    let operation: String
    let token: String?
    let args: [String: Value]
}
enum Value: Decodable {
    case string(String), number(Double), bool(Bool)
    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else { self = .number(try c.decode(Double.self)) }
    }
}
struct ProofError: Error { let code: String; let message: String }
func fail(_ code: String, _ message: String) throws -> Never { throw ProofError(code: code, message: message) }
func text(_ req: Request, _ key: String) throws -> String {
    guard case .string(let value) = req.args[key], !value.isEmpty else { try fail("INVALID_USAGE", "Missing string: \(key)") }
    return value
}
func number(_ req: Request, _ key: String) -> Double? { if case .number(let v) = req.args[key] { return v }; return nil }
func flag(_ req: Request, _ key: String) -> Bool { if case .bool(let v) = req.args[key] { return v }; return false }
func date(_ req: Request, _ key: String) throws -> Date {
    let s = try text(req, key)
    let formatter = ISO8601DateFormatter()
    guard let d = formatter.date(from: s) else { try fail("INVALID_USAGE", "\(key) must be an RFC3339 instant with an explicit offset and whole seconds") }
    return d
}
func iso(_ date: Date) -> String { ISO8601DateFormatter().string(from: date) }
func validToken(_ s: String) -> Bool { s.count == 32 && s.allSatisfy { "0123456789abcdef".contains($0) } }

struct Reference: Codable {
    let calendar: String
    let item: String
    let start: Double
}
func encodeReference(_ reference: Reference) throws -> String {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    return "ekp1." + (try encoder.encode(reference)).base64EncodedString()
}
func decodeReference(_ id: String) throws -> Reference {
    guard id.hasPrefix("ekp1."), let bytes = Data(base64Encoded: String(id.dropFirst(5))),
          let ref = try? JSONDecoder().decode(Reference.self, from: bytes), ref.start.isFinite else {
        try fail("INVALID_ID", "Expected an opaque ID from this native proof; rediscover legacy IDs")
    }
    return ref
}
func reference(_ event: EKEvent) throws -> String {
    try encodeReference(Reference(calendar: event.calendar.calendarIdentifier,
                                  item: event.calendarItemIdentifier,
                                  start: event.startDate.timeIntervalSince1970))
}
func calendar(_ store: EKEventStore, _ id: String) throws -> EKCalendar {
    guard let c = store.calendar(withIdentifier: id), c.allowedEntityTypes.contains(.event) else { try fail("NOT_FOUND", "Calendar ID is stale or unavailable; list calendars again") }
    return c
}
func resolve(_ store: EKEventStore, _ id: String) throws -> EKEvent {
    let ref = try decodeReference(id)
    let c = try calendar(store, ref.calendar)
    let start = Date(timeIntervalSince1970: ref.start)
    let predicate = store.predicateForEvents(withStart: start.addingTimeInterval(-1), end: start.addingTimeInterval(1), calendars: [c])
    let matches = store.events(matching: predicate).filter { $0.calendarItemIdentifier == ref.item && $0.startDate == start }
    guard matches.count == 1 else { try fail("NOT_FOUND", "Event reference is stale or ambiguous; rediscover it") }
    return matches[0]
}
// Compare native values, not JSON bytes: -0 and +0 denote the same offset.
struct AlarmState: Equatable {
    let type: EKAlarmType
    let offset: TimeInterval
    let absoluteDate: Date?
}
func alarmState(_ event: EKEvent) -> [AlarmState] {
    (event.alarms ?? []).map { AlarmState(type: $0.type, offset: $0.relativeOffset, absoluteDate: $0.absoluteDate) }
}
func alarmData(_ alarm: EKAlarm) -> [String: Any] {
    var result: [String: Any] = ["type": alarm.type.rawValue, "relative_seconds": alarm.relativeOffset]
    if let d = alarm.absoluteDate { result["absolute_date"] = iso(d) }
    return result
}
func eventData(_ event: EKEvent) throws -> [String: Any] {
    var result: [String: Any] = [
        "id": try reference(event), "calendar_id": event.calendar.calendarIdentifier,
        "title": event.title ?? "", "start": iso(event.startDate), "end": iso(event.endDate),
        "all_day": event.isAllDay, "recurring": event.hasRecurrenceRules || event.isDetached,
        "alarms": (event.alarms ?? []).map(alarmData), "time_zone": event.timeZone?.identifier ?? "floating"
    ]
    if let modified = event.lastModifiedDate { result["modified_at"] = iso(modified) }
    return result
}
func checkWriteScope(recurring: Bool, detached: Bool, writable: Bool) throws {
    guard !recurring && !detached else { try fail("UNSUPPORTED_OPERATION", "Recurring and detached writes remain blocked") }
    guard writable else { try fail("READ_ONLY", "Calendar is read-only") }
}
func owned(_ event: EKEvent, _ req: Request) throws {
    try checkWriteScope(recurring: event.hasRecurrenceRules, detached: event.isDetached,
                        writable: event.calendar.allowsContentModifications)
    guard let token = req.token, validToken(token), let url = event.url?.absoluteString,
          url.hasPrefix("acal-proof://\(token)/"), validToken(String(url.dropFirst("acal-proof://\(token)/".count))),
          event.notes == "acal packaged proof fixture" else { try fail("NOT_OWNED", "Only fixtures created with this --state-dir may be changed") }
    guard !event.isAllDay, !event.hasAttendees, event.organizer == nil,
          event.location == nil || event.location == "", event.structuredLocation == nil else {
        try fail("UNSUPPORTED_OPERATION", "This fixture acquired properties outside the proof's supported write scope")
    }
}
func alarmOffset(_ req: Request) throws -> Double? {
    guard let offset = number(req, "alarm_seconds") else { return nil }
    guard offset.isFinite, offset <= 0, offset >= -31_536_000, offset.truncatingRemainder(dividingBy: 60) == 0 else { try fail("INVALID_USAGE", "Reminder must be whole minutes before or at start, within one year") }
    return offset
}
