import Foundation
import EventKit

let store = EKEventStore()
var requestID = ""
var outcome = "rejected"
func perform(_ req: Request) throws -> [String: Any] {
    guard req.protocol == wireVersion, validToken(req.request_id) else { try fail("PROTOCOL_ERROR", "Incompatible native protocol or request ID") }
    if req.operation == "setup" {
        let currentAccess = EKEventStore.authorizationStatus(for: .event)
        if flag(req, "request_access"), currentAccess == .notDetermined || currentAccess == .writeOnly {
            var finished = false
            store.requestFullAccessToEvents { _, _ in DispatchQueue.main.async { finished = true } }
            while !finished { RunLoop.main.run(until: Date().addingTimeInterval(0.05)) }
        }
        let status = EKEventStore.authorizationStatus(for: .event)
        guard status == .fullAccess else { try fail("PERMISSION_REQUIRED", "Full Calendar Access is not granted. Run native setup --request-access interactively; if denied, enable Calendars access in System Settings for the responsible app") }
        return ["ready": true, "authorization": "full_access", "permission_persistence": "unqualified",
                "helper_build": ["version": Bundle.main.object(forInfoDictionaryKey: "ACALBuildVersion") as? String ?? "unpackaged",
                                 "date": Bundle.main.object(forInfoDictionaryKey: "ACALBuildDate") as? String ?? "unknown"]]
    }
    guard EKEventStore.authorizationStatus(for: .event) == .fullAccess else { try fail("PERMISSION_REQUIRED", "Full Calendar Access required; run native setup --request-access interactively") }
    switch req.operation {
    case "calendars":
        return ["calendars": store.calendars(for: .event).map { ["id": $0.calendarIdentifier, "name": $0.title, "writable": $0.allowsContentModifications, "source_type": $0.source.sourceType.rawValue] as [String: Any] }]
    case "list":
        let from = try date(req, "from"), to = try date(req, "to")
        var utc = Calendar(identifier: .gregorian); utc.timeZone = TimeZone(secondsFromGMT: 0)!
        guard to > from, to <= utc.date(byAdding: .year, value: 4, to: from)! else { try fail("INVALID_USAGE", "Query must have positive length and span at most four years") }
        let limit = number(req, "limit") ?? 100
        guard limit >= 1, limit <= 1000, limit.rounded() == limit else { try fail("INVALID_USAGE", "limit must be 1 through 1000") }
        let c = try calendar(store, text(req, "calendar"))
        let events = store.events(matching: store.predicateForEvents(withStart: from, end: to, calendars: [c])).filter { $0.startDate >= from && $0.startDate < to }.sorted {
            if $0.startDate != $1.startDate { return $0.startDate < $1.startDate }; return $0.calendarItemIdentifier < $1.calendarItemIdentifier
        }
        return ["events": try events.prefix(Int(limit)).map(eventData), "truncated": events.count > Int(limit), "selection": "start_in_half_open_range"]
    case "show": return try eventData(resolve(store, text(req, "id")))
    case "add":
        guard let token = req.token, validToken(token) else { try fail("INVALID_USAGE", "Fixture ownership token required") }
        let c = try calendar(store, text(req, "calendar"))
        guard c.allowsContentModifications else { try fail("READ_ONLY", "Calendar is read-only") }
        let start = try date(req, "start"), end = try date(req, "end")
        guard end > start else { try fail("INVALID_USAGE", "End must be after start") }
        let event = EKEvent(eventStore: store)
        event.calendar = c; event.title = try text(req, "title"); event.startDate = start; event.endDate = end
        event.timeZone = TimeZone(secondsFromGMT: 0); event.notes = "acal packaged proof fixture"
        event.url = URL(string: "acal-proof://\(token)/\(req.request_id)")
        if let offset = try alarmOffset(req) { event.alarms = [EKAlarm(relativeOffset: offset)] }
        let expectedTitle = event.title
        let expectedAlarms = alarmState(event)
        outcome = "unknown"
        try store.save(event, span: .thisEvent, commit: true)
        outcome = "applied_unverified"
        guard event.refresh(), event.title == expectedTitle, event.startDate == start, event.endDate == end,
              event.calendar.calendarIdentifier == c.calendarIdentifier,
              alarmState(event) == expectedAlarms else { try fail("READBACK_FAILED", "Created fixture could not be verified; inspect operation records before retrying") }
        try owned(event, req)
        let actual = try resolve(store, reference(event))
        return try eventData(actual)
    case "update", "remind", "delete":
        let event = try resolve(store, text(req, "id"))
        try owned(event, req)
        guard event.refresh() else { try fail("CONFLICT", "Event disappeared before mutation") }
        try owned(event, req)
        let start = event.startDate!, end = event.endDate!, cal = event.calendar.calendarIdentifier, url = event.url
        if req.operation == "delete" {
            let ref = try reference(event)
            outcome = "unknown"; try store.remove(event, span: .thisEvent, commit: true); outcome = "applied_unverified"
            // A fresh store checks absence; a permission failure is not absence.
            guard EKEventStore.authorizationStatus(for: .event) == .fullAccess else { try fail("READBACK_FAILED", "Permission changed before deletion verification") }
            do { _ = try resolve(EKEventStore(), ref); try fail("READBACK_FAILED", "Deleted fixture still resolves") }
            catch let error as ProofError where error.code == "NOT_FOUND" { return ["deleted_id": ref] }
        }
        if req.operation == "update" { event.title = try text(req, "title") }
        else {
            var alarms = (event.alarms ?? []).filter { $0.type != .display }
            if !flag(req, "clear") { guard let offset = try alarmOffset(req) else { try fail("INVALID_USAGE", "Reminder offset required") }; alarms.append(EKAlarm(relativeOffset: offset)) }
            event.alarms = alarms
        }
        let expectedTitle = event.title
        let expectedAlarms = alarmState(event)
        outcome = "unknown"; try store.save(event, span: .thisEvent, commit: true); outcome = "applied_unverified"
        guard event.refresh(), event.title == expectedTitle, event.startDate == start, event.endDate == end, event.url == url,
              event.calendar.calendarIdentifier == cal,
              alarmState(event) == expectedAlarms else { try fail("READBACK_FAILED", "Fixture write applied but readback differed; inspect before retrying") }
        try owned(event, req)
        return try eventData(resolve(store, reference(event)))
    default: try fail("UNSUPPORTED_OPERATION", "Unknown proof operation; production history and recurring writes are unsupported")
    }
}
var response: [String: Any] = ["protocol": wireVersion]
do {
    let input = FileHandle.standardInput.readDataToEndOfFile()
    guard input.count <= 1_048_576 else { try fail("PROTOCOL_ERROR", "Request exceeded limit") }
    let req = try JSONDecoder().decode(Request.self, from: input); requestID = req.request_id
    response["data"] = try perform(req); response["outcome"] = "verified"
} catch {
    let failure = error as? ProofError ?? ProofError(code: "NATIVE_FAILURE", message: "Native operation failed; inspect outcome and operation records before retrying")
    response["outcome"] = outcome
    response["error"] = ["code": failure.code, "message": failure.message]
}
response["request_id"] = requestID
if let bytes = try? JSONSerialization.data(withJSONObject: response, options: [.sortedKeys]) { FileHandle.standardOutput.write(bytes); FileHandle.standardOutput.write(Data([10])) }
else { exit(1) }
