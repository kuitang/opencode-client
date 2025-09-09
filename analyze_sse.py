#!/usr/bin/env python3
import json
import re
from collections import defaultdict

def analyze_sse_logs(log_file):
    """Analyze SSE logs for content erasure patterns"""
    
    # Track message parts by messageID and partID
    message_parts = defaultdict(lambda: defaultdict(list))
    
    # Pattern to match SSE data lines
    sse_pattern = re.compile(r'SSE line: data: (.+)')
    
    with open(log_file, 'r') as f:
        for line in f:
            match = sse_pattern.search(line)
            if match:
                try:
                    data = json.loads(match.group(1))
                    
                    # Look for message.part.updated events
                    if data.get('type') == 'message.part.updated':
                        props = data.get('properties', {})
                        part = props.get('part', {})
                        
                        message_id = part.get('messageID')
                        part_id = part.get('id')
                        text = part.get('text', '')
                        part_type = part.get('type')
                        
                        if message_id and part_id:
                            # Store the update
                            update = {
                                'text': text,
                                'type': part_type,
                                'full_data': part
                            }
                            message_parts[message_id][part_id].append(update)
                            
                except json.JSONDecodeError:
                    continue
    
    # Analyze for content erasure
    print("=== SSE MESSAGE PART ANALYSIS ===\n")
    
    erasure_found = False
    prefix_changes = []
    
    for message_id, parts in message_parts.items():
        print(f"Message ID: {message_id}")
        
        for part_id, updates in parts.items():
            print(f"  Part ID: {part_id}")
            print(f"  Number of updates: {len(updates)}")
            
            # Check for content erasure or prefix changes
            prev_text = ""
            for i, update in enumerate(updates):
                text = update['text']
                part_type = update['type']
                
                print(f"    Update {i+1}: type={part_type}, length={len(text)}")
                
                # Check if new text is NOT a continuation of previous
                if prev_text and text:
                    if not text.startswith(prev_text):
                        erasure_found = True
                        print(f"      ⚠️  CONTENT CHANGE DETECTED!")
                        print(f"      Previous: '{prev_text[:50]}...' (len={len(prev_text)})")
                        print(f"      Current:  '{text[:50]}...' (len={len(text)})")
                        
                        # Check if it's a prefix change
                        common_prefix = ""
                        for j in range(min(len(prev_text), len(text))):
                            if prev_text[j] == text[j]:
                                common_prefix += prev_text[j]
                            else:
                                break
                        
                        if common_prefix:
                            print(f"      Common prefix: '{common_prefix}' (len={len(common_prefix)})")
                            print(f"      Divergence point: prev[{len(common_prefix)}:] vs curr[{len(common_prefix)}:]")
                            prefix_changes.append({
                                'message_id': message_id,
                                'part_id': part_id,
                                'update_num': i+1,
                                'prev': prev_text,
                                'curr': text,
                                'common_prefix': common_prefix
                            })
                
                # Show first and last 30 chars of each update
                if text:
                    if len(text) <= 60:
                        print(f"      Text: '{text}'")
                    else:
                        print(f"      Text: '{text[:30]}...{text[-30:]}'")
                
                prev_text = text
            
            print()
    
    # Summary
    print("\n=== SUMMARY ===")
    if erasure_found:
        print("⚠️ CONTENT ERASURE/CHANGES DETECTED!")
        print(f"Found {len(prefix_changes)} instances where content was not strictly additive:\n")
        
        for change in prefix_changes:
            print(f"  - Message {change['message_id']}, Part {change['part_id']}, Update {change['update_num']}")
            print(f"    Previous ended with: '...{change['prev'][-50:]}'")
            print(f"    Current starts with: '{change['curr'][:50]}...'")
            print()
    else:
        print("✅ No content erasure detected - all updates are strictly additive")
    
    return message_parts, prefix_changes

if __name__ == "__main__":
    import sys
    
    log_file = sys.argv[1] if len(sys.argv) > 1 else "server.log"
    
    print(f"Analyzing SSE logs from: {log_file}\n")
    message_parts, prefix_changes = analyze_sse_logs(log_file)
    
    # Additional analysis - check for duplicate part IDs
    print("\n=== DUPLICATE PART ID ANALYSIS ===")
    for message_id, parts in message_parts.items():
        if len(parts) > 1:
            print(f"Message {message_id} has {len(parts)} different part IDs:")
            for part_id in parts.keys():
                print(f"  - {part_id}")