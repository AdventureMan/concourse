module Colors exposing
    ( aborted
    , abortedFaded
    , asciiArt
    , background
    , backgroundDark
    , border
    , bottomBarText
    , buildStatusColor
    , buildTooltipText
    , buttonDisabledGrey
    , card
    , cliIconHover
    , dashboardPipelineHeaderText
    , dashboardText
    , dashboardTooltipBackground
    , dropdownFaded
    , dropdownItemInputText
    , dropdownItemSelectedBackground
    , dropdownItemSelectedText
    , dropdownUnselectedText
    , error
    , errorFaded
    , errorLog
    , failure
    , failureFaded
    , flySuccessButtonHover
    , flySuccessCard
    , flySuccessTokenCopied
    , frame
    , groupBackground
    , groupBorderHovered
    , groupBorderSelected
    , groupBorderUnselected
    , groupsBarBackground
    , hamburgerClosedBackground
    , infoBarBackground
    , inputOutline
    , noPipelinesPlaceholderBackground
    , paginationHover
    , paused
    , pending
    , pendingFaded
    , pinHighlight
    , pinIconHover
    , pinTools
    , pinned
    , resourceError
    , retryTabText
    , secondaryTopBar
    , sectionHeader
    , showArchivedButtonBorder
    , sideBar
    , sideBarActive
    , sideBarHovered
    , sideBarTooltipBackground
    , started
    , startedFaded
    , statusColor
    , success
    , successFaded
    , text
    , tooltipBackground
    , topBarBackground
    , unknown
    , white
    )

import ColorValues
import Concourse.BuildStatus exposing (BuildStatus(..))
import Concourse.PipelineStatus exposing (PipelineStatus(..))



----


frame : String
frame =
    "#1e1d1d"


topBarBackground : String
topBarBackground =
    ColorValues.grey100


infoBarBackground : String
infoBarBackground =
    ColorValues.grey100


hamburgerClosedBackground : String
hamburgerClosedBackground =
    ColorValues.grey100


border : String
border =
    ColorValues.black


sideBarTooltipBackground : String
sideBarTooltipBackground =
    ColorValues.grey20


dropdownItemSelectedBackground : String
dropdownItemSelectedBackground =
    ColorValues.grey90



----


sectionHeader : String
sectionHeader =
    "#1e1d1d"



----


dashboardText : String
dashboardText =
    "#ffffff"


dashboardPipelineHeaderText : String
dashboardPipelineHeaderText =
    ColorValues.grey20


dropdownItemInputText : String
dropdownItemInputText =
    ColorValues.grey20


dropdownItemSelectedText : String
dropdownItemSelectedText =
    ColorValues.grey30



----


bottomBarText : String
bottomBarText =
    ColorValues.grey40



----


pinned : String
pinned =
    "#5c3bd1"



----


tooltipBackground : String
tooltipBackground =
    "#9b9b9b"


dashboardTooltipBackground : String
dashboardTooltipBackground =
    ColorValues.grey20



----


pinIconHover : String
pinIconHover =
    "#1e1d1d"



----


pinTools : String
pinTools =
    "#2e2c2c"



----


white : String
white =
    ColorValues.white



----


background : String
background =
    "#3d3c3c"


noPipelinesPlaceholderBackground : String
noPipelinesPlaceholderBackground =
    ColorValues.grey80


showArchivedButtonBorder : String
showArchivedButtonBorder =
    ColorValues.grey90



----


backgroundDark : String
backgroundDark =
    ColorValues.grey80



----


started : String
started =
    "#fad43b"



----


startedFaded : String
startedFaded =
    "#f1c40f"



----


success : String
success =
    "#11c560"



----


successFaded : String
successFaded =
    "#419867"



----


paused : String
paused =
    "#3498db"


pending : String
pending =
    "#9b9b9b"


pendingFaded : String
pendingFaded =
    "#7a7373"


unknown : String
unknown =
    "#9b9b9b"


failure : String
failure =
    "#ed4b35"


failureFaded : String
failureFaded =
    "#bd3826"


error : String
error =
    "#f5a623"


errorFaded : String
errorFaded =
    "#ec9910"


aborted : String
aborted =
    "#8b572a"


abortedFaded : String
abortedFaded =
    "#6a401c"



-----


card : String
card =
    ColorValues.grey90



-----


secondaryTopBar : String
secondaryTopBar =
    "#2a2929"


flySuccessCard : String
flySuccessCard =
    "#323030"


flySuccessButtonHover : String
flySuccessButtonHover =
    "#242424"


flySuccessTokenCopied : String
flySuccessTokenCopied =
    "#196ac8"



-----


resourceError : String
resourceError =
    ColorValues.error40



-----


cliIconHover : String
cliIconHover =
    ColorValues.white



-----


text : String
text =
    "#e6e7e8"



-----


asciiArt : String
asciiArt =
    ColorValues.grey50



-----


paginationHover : String
paginationHover =
    "#504b4b"



----


inputOutline : String
inputOutline =
    ColorValues.grey60



-----


groupsBarBackground : String
groupsBarBackground =
    "#2b2a2a"



----


buildTooltipText : String
buildTooltipText =
    "#ecf0f1"



----


dropdownFaded : String
dropdownFaded =
    ColorValues.grey80



----


dropdownUnselectedText : String
dropdownUnselectedText =
    ColorValues.grey40



----


groupBorderSelected : String
groupBorderSelected =
    "#979797"


groupBorderUnselected : String
groupBorderUnselected =
    "#2b2a2a"


groupBorderHovered : String
groupBorderHovered =
    "#fff2"


groupBackground : String
groupBackground =
    "rgba(151, 151, 151, 0.1)"



----


sideBar : String
sideBar =
    "#333333"


sideBarActive : String
sideBarActive =
    "#272727"


sideBarHovered : String
sideBarHovered =
    "#444444"


errorLog : String
errorLog =
    "#e74c3c"


retryTabText : String
retryTabText =
    "#f5f5f5"


statusColor : PipelineStatus -> String
statusColor status =
    case status of
        PipelineStatusPaused ->
            paused

        PipelineStatusSucceeded _ ->
            success

        PipelineStatusPending _ ->
            pending

        PipelineStatusFailed _ ->
            failure

        PipelineStatusErrored _ ->
            error

        PipelineStatusAborted _ ->
            aborted


buildStatusColor : Bool -> BuildStatus -> String
buildStatusColor isBright status =
    if isBright then
        case status of
            BuildStatusStarted ->
                started

            BuildStatusPending ->
                pending

            BuildStatusSucceeded ->
                success

            BuildStatusFailed ->
                failure

            BuildStatusErrored ->
                error

            BuildStatusAborted ->
                aborted

    else
        case status of
            BuildStatusStarted ->
                startedFaded

            BuildStatusPending ->
                pendingFaded

            BuildStatusSucceeded ->
                successFaded

            BuildStatusFailed ->
                failureFaded

            BuildStatusErrored ->
                errorFaded

            BuildStatusAborted ->
                abortedFaded


buttonDisabledGrey : String
buttonDisabledGrey =
    "#979797"
